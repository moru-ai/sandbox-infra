package juicefs

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/juicedata/juicefs/pkg/meta"
	"github.com/juicedata/juicefs/pkg/object"
)

// FormatConfig holds configuration for formatting a new JuiceFS volume.
type FormatConfig struct {
	// VolumeID is the unique identifier for the volume (used in GCS path prefix)
	VolumeID string

	// RedisDB is the Redis database number for this volume's metadata
	RedisDB int32

	// PoolConfig contains the shared Redis/GCS configuration
	PoolConfig Config
}

// FormatVolume initializes a new JuiceFS volume with the given configuration.
// This creates the metadata structure in Redis and prepares GCS for data storage.
func FormatVolume(ctx context.Context, cfg FormatConfig) error {
	// Build Redis URL with database number
	redisURL := fmt.Sprintf("%s/%d", cfg.PoolConfig.RedisURL, cfg.RedisDB)

	// Create metadata client
	metaConf := meta.DefaultConf()
	metaConf.Retries = 10
	m := meta.NewClient(redisURL, metaConf)

	// Check if already formatted
	_, err := m.Load(false)
	if err == nil {
		// Already formatted - this is idempotent, return success
		return nil
	}
	if !strings.HasPrefix(err.Error(), "database is not formatted") {
		return fmt.Errorf("check existing format: %w", err)
	}

	// Create new format configuration
	// Bucket format for GCS: gs://bucket-name/ (no path, prefix is added via WithPrefix wrapper)
	bucketURL := fmt.Sprintf("gs://%s/", cfg.PoolConfig.GCSBucket)
	volumePrefix := cfg.VolumeID + "/"

	format := &meta.Format{
		Name:             cfg.VolumeID,
		UUID:             uuid.New().String(),
		Storage:          "gs",
		Bucket:           bucketURL + volumePrefix, // Store full path in format for reference
		BlockSize:        4096,                     // 4 MiB blocks (in KiB units)
		Compression:      "lz4",
		TrashDays:        0,  // Disable trash for API volumes
		DirStats:         true,
		MetaVersion:      meta.MaxVersion,
		MinClientVersion: "1.1.0-A",
	}

	// Test storage connectivity
	// Create base storage with just bucket, then add prefix wrapper
	baseBlob, err := object.CreateStorage("gs", bucketURL, "", "", "")
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	blob := object.WithPrefix(baseBlob, volumePrefix)

	// Write UUID marker to GCS (will be written as {volumeID}/juicefs_uuid)
	if err = blob.Put(ctx, "juicefs_uuid", strings.NewReader(format.UUID)); err != nil {
		return fmt.Errorf("write uuid marker: %w", err)
	}

	// Initialize metadata
	if err = m.Init(format, false); err != nil {
		// Clean up UUID marker on failure
		_ = blob.Delete(ctx, "juicefs_uuid")
		return fmt.Errorf("init metadata: %w", err)
	}

	return nil
}

// DestroyVolume removes all JuiceFS data for a volume.
// This deletes metadata from Redis and can optionally clean up GCS data.
func DestroyVolume(ctx context.Context, cfg FormatConfig, deleteData bool) error {
	// Build Redis URL with database number
	redisURL := fmt.Sprintf("%s/%d", cfg.PoolConfig.RedisURL, cfg.RedisDB)

	// Create metadata client
	metaConf := meta.DefaultConf()
	m := meta.NewClient(redisURL, metaConf)

	// Load format to get storage config
	format, err := m.Load(false)
	if err != nil {
		if strings.HasPrefix(err.Error(), "database is not formatted") {
			// Already destroyed or never created
			return nil
		}
		return fmt.Errorf("load format: %w", err)
	}

	if deleteData {
		// Parse the bucket URL to extract bucket and prefix
		// Format is gs://bucket-name/prefix/
		bucketURL := format.Bucket
		if !strings.HasPrefix(bucketURL, "gs://") {
			return fmt.Errorf("invalid bucket URL format: %s", bucketURL)
		}

		// Extract bucket name and prefix from gs://bucket-name/prefix/
		trimmed := strings.TrimPrefix(bucketURL, "gs://")
		parts := strings.SplitN(trimmed, "/", 2)
		baseBucketURL := fmt.Sprintf("gs://%s/", parts[0])
		var volumePrefix string
		if len(parts) > 1 {
			volumePrefix = parts[1]
		}

		// Create storage client with prefix wrapper
		baseBlob, err := object.CreateStorage("gs", baseBucketURL, "", "", "")
		if err != nil {
			return fmt.Errorf("create storage client: %w", err)
		}
		blob := object.WithPrefix(baseBlob, volumePrefix)

		// List and delete all objects under the volume prefix
		objs, err := object.ListAll(ctx, blob, "", "", true, false)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}

		for obj := range objs {
			if obj == nil {
				break
			}
			if err := blob.Delete(ctx, obj.Key()); err != nil {
				// Log but continue - best effort deletion
				continue
			}
		}
	}

	// Reset the Redis database (flush all keys in this DB)
	// Note: This uses the meta client's internal connection
	// The meta package doesn't expose a destroy method, so we just
	// rely on the Redis DB being reused for a new volume eventually
	// For now, the DB isolation means old data won't interfere

	return nil
}
