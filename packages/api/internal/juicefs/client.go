// Package juicefs provides a wrapper around JuiceFS Go packages for file operations.
// This allows the API server to perform file operations (list, download, upload, delete)
// on volumes without running a FUSE mount - it uses JuiceFS's internal Go APIs directly.
//
// Reference implementation: ~/moru/juicefs/cmd/gateway.go (initForSvc function)
package juicefs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/juicedata/juicefs/pkg/chunk"
	"github.com/juicedata/juicefs/pkg/fs"
	"github.com/juicedata/juicefs/pkg/meta"
	"github.com/juicedata/juicefs/pkg/object"
	"github.com/juicedata/juicefs/pkg/vfs"
)

// Config holds configuration for JuiceFS connections.
type Config struct {
	// RedisURL is the base connection URL for Redis metadata (e.g., redis://host:6379)
	// The database number will be appended based on the volume's redis_db
	RedisURL string

	// RedisPassword is the password for Redis authentication
	RedisPassword string

	// GCSBucket is the GCS bucket name for data storage
	GCSBucket string

	// GCSProject is the GCP project ID (optional, uses default credentials)
	GCSProject string
}

// FileInfo represents metadata about a file or directory.
type FileInfo struct {
	Name       string
	Path       string
	Type       string // "file" or "directory"
	Size       int64
	ModifiedAt time.Time
}

// Client provides file operations for a single volume.
type Client struct {
	volumeID string
	redisDB  int32
	config   Config

	jfs     *fs.FileSystem
	metaCli meta.Meta
	store   chunk.ChunkStore
	blob    object.ObjectStorage

	mu     sync.RWMutex
	closed bool
}

// NewClient creates a new JuiceFS client for a volume.
// It initializes connections to Redis (metadata) and GCS (data storage).
func NewClient(volumeID string, redisDB int32, config Config) (*Client, error) {
	c := &Client{
		volumeID: volumeID,
		redisDB:  redisDB,
		config:   config,
	}

	// Build Redis URL with database number
	// Format: redis://:password@host:port/db
	redisURL := fmt.Sprintf("%s/%d", config.RedisURL, redisDB)

	// Create metadata client configuration
	metaConf := meta.DefaultConf()
	metaConf.Retries = 10
	metaConf.ReadOnly = false
	metaConf.NoBGJob = true // No background jobs for API server usage

	// Create metadata client
	c.metaCli = meta.NewClient(redisURL, metaConf)

	// Load volume format from metadata
	format, err := c.metaCli.Load(true)
	if err != nil {
		return nil, fmt.Errorf("load volume format: %w", err)
	}

	// Create GCS object storage
	// Use GCS JSON API endpoint
	bucketEndpoint := fmt.Sprintf("https://storage.googleapis.com/%s/%s/", config.GCSBucket, volumeID)
	c.blob, err = object.CreateStorage("gs", bucketEndpoint, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("create GCS storage: %w", err)
	}

	// Create chunk configuration with sensible defaults for API server usage
	chunkConf := &chunk.Config{
		BlockSize:   format.BlockSize * 1024,
		Compress:    format.Compression,
		HashPrefix:  format.HashPrefix,
		MaxUpload:   4,
		MaxDownload: 4,
		BufferSize:  64 << 20, // 64 MiB
		GetTimeout:  time.Minute,
		PutTimeout:  time.Minute * 5,
		MaxRetries:  10,
	}

	// Create chunk store
	c.store = chunk.NewCachedStore(c.blob, *chunkConf, nil)

	// Register metadata message handlers
	c.metaCli.OnMsg(meta.DeleteSlice, func(args ...interface{}) error {
		return c.store.Remove(args[0].(uint64), int(args[1].(uint32)))
	})

	// Create metadata session
	if err := c.metaCli.NewSession(true); err != nil {
		return nil, fmt.Errorf("create meta session: %w", err)
	}

	// Create VFS configuration
	vfsConf := &vfs.Config{
		Meta:   metaConf,
		Format: *format,
		Chunk:  chunkConf,
	}

	// Create filesystem
	c.jfs, err = fs.NewFileSystem(vfsConf, c.metaCli, c.store, nil)
	if err != nil {
		c.metaCli.CloseSession()
		return nil, fmt.Errorf("create filesystem: %w", err)
	}

	return c, nil
}

// Close releases resources associated with this client.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	var errs []error

	if c.metaCli != nil {
		if err := c.metaCli.CloseSession(); err != nil {
			errs = append(errs, fmt.Errorf("close meta session: %w", err))
		}
	}

	// Note: ChunkStore doesn't have a Close method, cleanup is handled by GC

	if len(errs) > 0 {
		return fmt.Errorf("errors closing client: %v", errs)
	}
	return nil
}

// metaCtx returns a meta.Context for JuiceFS operations.
func (c *Client) metaCtx(ctx context.Context) meta.Context {
	// Use uid=0, gid=0 (root) for API operations
	return meta.NewContext(uint32(os.Getpid()), 0, []uint32{0})
}

// ListDir lists files and directories at the given path.
func (c *Client) ListDir(ctx context.Context, path string) ([]FileInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("client closed")
	}

	mctx := c.metaCtx(ctx)

	// Open directory
	f, errno := c.jfs.Open(mctx, path, 0)
	if errno != 0 {
		if errno == syscall.ENOENT {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		return nil, fmt.Errorf("open directory: %s", errno)
	}
	defer f.Close(mctx)

	// Read directory entries
	entries, errno := f.ReaddirPlus(mctx, 0)
	if errno != 0 {
		return nil, fmt.Errorf("read directory: %s", errno)
	}

	// Convert to FileInfo slice
	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		fi := FileInfo{
			Name:       string(entry.Name),
			Path:       filepath.Join(path, string(entry.Name)),
			Size:       int64(entry.Attr.Length),
			ModifiedAt: time.Unix(entry.Attr.Mtime, int64(entry.Attr.Mtimensec)),
		}
		if entry.Attr.Typ == meta.TypeDirectory {
			fi.Type = "directory"
		} else {
			fi.Type = "file"
		}
		result = append(result, fi)
	}

	return result, nil
}

// jfsReader wraps a JuiceFS file handle for reading.
type jfsReader struct {
	file   *fs.File
	ctx    meta.Context
	offset int64
	size   int64
}

func (r *jfsReader) Read(p []byte) (n int, err error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}

	// Read from current offset
	n, err = r.file.Pread(r.ctx, p, r.offset)
	if err != nil && err != io.EOF {
		return 0, err
	}

	r.offset += int64(n)

	if n == 0 && r.offset >= r.size {
		return 0, io.EOF
	}

	return n, nil
}

func (r *jfsReader) Close() error {
	errno := r.file.Close(r.ctx)
	if errno != 0 {
		return fmt.Errorf("close error: %s", errno)
	}
	return nil
}

// Download streams file content from the given path.
func (c *Client) Download(ctx context.Context, path string) (io.ReadCloser, int64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, 0, fmt.Errorf("client closed")
	}

	mctx := c.metaCtx(ctx)

	// Open file for reading
	f, errno := c.jfs.Open(mctx, path, vfs.MODE_MASK_R)
	if errno != 0 {
		if errno == syscall.ENOENT {
			return nil, 0, fmt.Errorf("file not found: %s", path)
		}
		return nil, 0, fmt.Errorf("open file: %s", errno)
	}

	// Get file info for size using Stat()
	info, err := f.Stat()
	if err != nil {
		f.Close(mctx)
		return nil, 0, fmt.Errorf("stat file: %w", err)
	}
	size := info.Size()

	reader := &jfsReader{
		file:   f,
		ctx:    mctx,
		offset: 0,
		size:   size,
	}

	return reader, size, nil
}

// Upload streams content to a file at the given path.
// Creates parent directories as needed.
func (c *Client) Upload(ctx context.Context, path string, content io.Reader) (int64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return 0, fmt.Errorf("client closed")
	}

	mctx := c.metaCtx(ctx)

	// Create parent directories
	dir := filepath.Dir(path)
	if dir != "/" && dir != "." {
		errno := c.jfs.MkdirAll(mctx, dir, 0o755, 0o022)
		if errno != 0 && errno != syscall.EEXIST {
			return 0, fmt.Errorf("create directories: %s", errno)
		}
	}

	// Create file
	f, errno := c.jfs.Create(mctx, path, 0o644, 0o022)
	if errno != 0 {
		return 0, fmt.Errorf("create file: %s", errno)
	}
	defer f.Close(mctx)

	// Write content
	buf := make([]byte, 128*1024) // 128 KiB buffer
	var totalWritten int64
	var offset int64

	for {
		n, err := content.Read(buf)
		if n > 0 {
			written, errno := f.Pwrite(mctx, buf[:n], offset)
			if errno != 0 {
				return totalWritten, fmt.Errorf("write error: %s", errno)
			}
			offset += int64(written)
			totalWritten += int64(written)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return totalWritten, fmt.Errorf("read content: %w", err)
		}
	}

	// Flush writes
	errno = f.Flush(mctx)
	if errno != 0 {
		return totalWritten, fmt.Errorf("flush: %s", errno)
	}

	return totalWritten, nil
}

// Delete removes a file or directory at the given path.
func (c *Client) Delete(ctx context.Context, path string, recursive bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("client closed")
	}

	mctx := c.metaCtx(ctx)

	if recursive {
		// Recursive delete: skipTrash=true, numthreads=1
		errno := c.jfs.Rmr(mctx, path, true, 1)
		if errno != 0 {
			if errno == syscall.ENOENT {
				return nil // Already deleted
			}
			return fmt.Errorf("recursive delete: %s", errno)
		}
	} else {
		// Single file/empty directory delete
		errno := c.jfs.Delete(mctx, path)
		if errno != 0 {
			if errno == syscall.ENOENT {
				return nil // Already deleted
			}
			return fmt.Errorf("delete: %s", errno)
		}
	}

	return nil
}
