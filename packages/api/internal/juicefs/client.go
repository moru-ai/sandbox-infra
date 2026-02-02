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
	"sort"
	"sync"
	"syscall"
	"time"

	"cloud.google.com/go/storage"
	"github.com/juicedata/juicefs/pkg/chunk"
	"github.com/juicedata/juicefs/pkg/fs"
	"github.com/juicedata/juicefs/pkg/meta"
	"github.com/juicedata/juicefs/pkg/object"
	"github.com/juicedata/juicefs/pkg/vfs"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

// Config holds configuration for JuiceFS connections.
type Config struct {
	// GCSBucket is the GCS bucket name for data and metadata storage
	GCSBucket string
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
	config   Config

	jfs     *fs.FileSystem
	metaCli meta.Meta
	store   chunk.ChunkStore
	blob    object.ObjectStorage

	// Local SQLite file path (for cleanup and sync)
	sqlitePath string
	tmpDir     string

	mu     sync.RWMutex
	closed bool
}

// NewClient creates a new JuiceFS client for a volume.
// Downloads SQLite metadata from GCS and initializes JuiceFS for file operations.
func NewClient(volumeID string, _ int32, config Config) (*Client, error) {
	ctx := context.Background()

	// Create temporary directory for SQLite file
	tmpDir, err := os.MkdirTemp("", "juicefs-client-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	sqlitePath := filepath.Join(tmpDir, "meta.db")

	// Download SQLite metadata from GCS
	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create GCS client: %w", err)
	}
	defer gcsClient.Close()

	_, metaPrefix := gcsPathsForVolume(config.GCSBucket, volumeID)
	bucket := gcsClient.Bucket(config.GCSBucket)
	metaObj := bucket.Object(metaPrefix + "meta.db")

	reader, err := metaObj.NewReader(ctx)
	if err != nil {
		os.RemoveAll(tmpDir)
		if err == storage.ErrObjectNotExist {
			return nil, fmt.Errorf("volume metadata not found: %s", volumeID)
		}
		return nil, fmt.Errorf("download metadata: %w", err)
	}

	sqliteFile, err := os.Create(sqlitePath)
	if err != nil {
		reader.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create sqlite file: %w", err)
	}

	if _, err = io.Copy(sqliteFile, reader); err != nil {
		sqliteFile.Close()
		reader.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("write sqlite file: %w", err)
	}
	sqliteFile.Close()
	reader.Close()

	logger.L().Info(ctx, "Downloaded volume metadata",
		zap.String("volume_id", volumeID),
		zap.String("path", sqlitePath))

	// Create metadata client with SQLite backend
	sqliteURL := "sqlite3://" + sqlitePath + "?cache=shared&_journal=WAL&_timeout=5000"
	metaConf := meta.DefaultConf()
	metaConf.Retries = 10
	metaConf.ReadOnly = false
	metaCli := meta.NewClient(sqliteURL, metaConf)

	// Load format from metadata
	format, err := metaCli.Load(true)
	if err != nil {
		metaCli.Shutdown()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("load format: %w", err)
	}

	logger.L().Info(ctx, "Loaded volume format",
		zap.String("volume_id", volumeID),
		zap.String("name", format.Name),
		zap.String("storage", format.Storage),
		zap.String("bucket", format.Bucket))

	// Create object storage for data
	blob, err := object.CreateStorage(format.Storage, format.Bucket, format.AccessKey, format.SecretKey, format.SessionToken)
	if err != nil {
		metaCli.Shutdown()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create storage: %w", err)
	}

	// Create chunk store
	chunkConf := chunk.Config{
		BlockSize:  format.BlockSize * 1024, // Convert KiB to bytes
		Compress:   format.Compression,
		GetTimeout: 60 * time.Second,
		PutTimeout: 60 * time.Second,
		MaxUpload:  20,
		MaxRetries: 10,
		BufferSize: 300 << 20, // 300 MiB
	}
	// Use nil registerer to avoid metric conflicts between volumes
	store := chunk.NewCachedStore(blob, chunkConf, nil)

	// Start metadata session
	if err = metaCli.NewSession(false); err != nil {
		metaCli.Shutdown()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("new session: %w", err)
	}

	// Create VFS config
	vfsConf := &vfs.Config{
		Meta:       metaConf,
		Format:     *format,
		Chunk:      &chunkConf,
		AttrTimeout: time.Second,
		EntryTimeout: time.Second,
		DirEntryTimeout: time.Second,
	}

	// Create FileSystem (pass nil registry to avoid metric conflicts)
	jfs, err := fs.NewFileSystem(vfsConf, metaCli, store, nil)
	if err != nil {
		metaCli.CloseSession()
		metaCli.Shutdown()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create filesystem: %w", err)
	}

	logger.L().Info(ctx, "JuiceFS client initialized",
		zap.String("volume_id", volumeID))

	return &Client{
		volumeID:   volumeID,
		config:     config,
		jfs:        jfs,
		metaCli:    metaCli,
		store:      store,
		blob:       blob,
		sqlitePath: sqlitePath,
		tmpDir:     tmpDir,
		closed:     false,
	}, nil
}

// Close releases resources associated with this client.
// It syncs metadata to GCS before closing.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	var errs []error

	// Sync metadata to GCS before closing
	if err := c.syncToGCSLocked(); err != nil {
		errs = append(errs, fmt.Errorf("sync to GCS: %w", err))
	}

	if c.metaCli != nil {
		if err := c.metaCli.CloseSession(); err != nil {
			errs = append(errs, fmt.Errorf("close meta session: %w", err))
		}
		if err := c.metaCli.Shutdown(); err != nil {
			errs = append(errs, fmt.Errorf("shutdown meta: %w", err))
		}
	}

	// Cleanup temp directory
	if c.tmpDir != "" {
		os.RemoveAll(c.tmpDir)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing client: %v", errs)
	}
	return nil
}

// SyncToGCS uploads the current SQLite metadata to GCS.
// This should be called after write operations to persist changes.
func (c *Client) SyncToGCS() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.syncToGCSLocked()
}

// syncToGCSLocked uploads SQLite metadata to GCS (must hold lock).
func (c *Client) syncToGCSLocked() error {
	if c.sqlitePath == "" {
		return nil
	}

	ctx := context.Background()

	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create GCS client: %w", err)
	}
	defer gcsClient.Close()

	_, metaPrefix := gcsPathsForVolume(c.config.GCSBucket, c.volumeID)
	bucket := gcsClient.Bucket(c.config.GCSBucket)
	metaObj := bucket.Object(metaPrefix + "meta.db")

	// Read local SQLite file
	sqliteFile, err := os.Open(c.sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite file: %w", err)
	}
	defer sqliteFile.Close()

	// Upload to GCS
	writer := metaObj.NewWriter(ctx)
	if _, err = io.Copy(writer, sqliteFile); err != nil {
		writer.Close()
		return fmt.Errorf("upload metadata: %w", err)
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("close GCS writer: %w", err)
	}

	logger.L().Debug(ctx, "Synced metadata to GCS",
		zap.String("volume_id", c.volumeID))

	return nil
}

// metaCtx returns a meta.Context for JuiceFS operations.
func (c *Client) metaCtx(ctx context.Context) meta.Context {
	// Use uid=0, gid=0 (root) for API operations
	return meta.NewContext(uint32(os.Getpid()), 0, []uint32{0})
}

// ListDirResult contains the result of a directory listing with pagination info.
type ListDirResult struct {
	Files   []FileInfo
	HasMore bool
}

// ListDir lists files and directories at the given path with optional pagination.
// If limit is 0, all entries are returned. offset specifies how many entries to skip.
func (c *Client) ListDir(ctx context.Context, path string, limit, offset int) (*ListDirResult, error) {
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

	// Sort entries by name for consistent pagination
	sort.Slice(entries, func(i, j int) bool {
		return string(entries[i].Name) < string(entries[j].Name)
	})

	// Apply pagination
	totalEntries := len(entries)
	hasMore := false

	// Apply offset
	if offset > 0 {
		if offset >= totalEntries {
			return &ListDirResult{Files: []FileInfo{}, HasMore: false}, nil
		}
		entries = entries[offset:]
	}

	// Apply limit
	if limit > 0 && len(entries) > limit {
		hasMore = true
		entries = entries[:limit]
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

	return &ListDirResult{Files: result, HasMore: hasMore}, nil
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
// After upload, syncs metadata to GCS.
func (c *Client) Upload(ctx context.Context, path string, content io.Reader) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	// Truncate to 0 to handle overwrites (Create doesn't truncate existing files)
	errno = c.jfs.Truncate(mctx, path, 0)
	if errno != 0 {
		return 0, fmt.Errorf("truncate file: %s", errno)
	}

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

	// Sync metadata to GCS after write
	if err := c.syncToGCSLocked(); err != nil {
		logger.L().Warn(ctx, "Failed to sync metadata to GCS after upload",
			zap.Error(err),
			zap.String("volume_id", c.volumeID),
			zap.String("path", path))
	}

	return totalWritten, nil
}

// Delete removes a file or directory at the given path.
// After deletion, syncs metadata to GCS.
func (c *Client) Delete(ctx context.Context, path string, recursive bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	// Sync metadata to GCS after delete
	if err := c.syncToGCSLocked(); err != nil {
		logger.L().Warn(ctx, "Failed to sync metadata to GCS after delete",
			zap.Error(err),
			zap.String("volume_id", c.volumeID),
			zap.String("path", path))
	}

	return nil
}
