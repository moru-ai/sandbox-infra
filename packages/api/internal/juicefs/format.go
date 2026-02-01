package juicefs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/juicedata/juicefs/pkg/meta"
	"github.com/juicedata/juicefs/pkg/object"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

// FormatConfig holds configuration for formatting a new JuiceFS volume.
type FormatConfig struct {
	// VolumeID is the unique identifier for the volume (used in GCS path prefix)
	VolumeID string

	// RedisDB is the Redis database number for this volume's metadata
	RedisDB int32

	// Password is the per-volume Redis ACL password for db_{RedisDB} user
	Password string

	// PoolConfig contains the shared Redis/GCS configuration
	PoolConfig Config

	// MetadataRedisClient is the Redis client for metadata cleanup operations.
	// This should be the volumesRedisClient from the handler.
	// If nil, metadata cleanup will be skipped during destroy.
	MetadataRedisClient redis.UniversalClient
}

// buildRedisURL constructs a Redis URL with per-volume ACL credentials.
// Input: rediss://host:port?query -> rediss://db_{redisDB}:{password}@host:port/{redisDB}?query
func buildRedisURL(baseURL string, redisDB int32, password string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse redis URL: %w", err)
	}

	// Set per-volume ACL credentials: username=db_{redisDB}, password=password
	aclUser := fmt.Sprintf("db_%d", redisDB)
	u.User = url.UserPassword(aclUser, password)

	// Append database number to path
	u.Path = fmt.Sprintf("/%d", redisDB)

	return u.String(), nil
}

// FormatVolume initializes a new JuiceFS volume with the given configuration.
// This creates the metadata structure in Redis and prepares GCS for data storage.
func FormatVolume(ctx context.Context, cfg FormatConfig) error {
	// Build Redis URL with per-volume ACL credentials
	redisURL, err := buildRedisURL(cfg.PoolConfig.RedisURL, cfg.RedisDB, cfg.Password)
	if err != nil {
		return fmt.Errorf("build redis URL: %w", err)
	}

	// Create metadata client
	metaConf := meta.DefaultConf()
	metaConf.Retries = 10
	m := meta.NewClient(redisURL, metaConf)

	// Check if already formatted
	_, err = m.Load(false)
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
		TrashDays:        0, // Disable trash for API volumes
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
	// Build Redis URL with per-volume ACL credentials
	redisURL, err := buildRedisURL(cfg.PoolConfig.RedisURL, cfg.RedisDB, cfg.Password)
	if err != nil {
		return fmt.Errorf("build redis URL: %w", err)
	}

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

	// Clean up Redis metadata keys for this volume
	// JuiceFS uses keys with pattern {N}* where N is the Redis DB number
	// e.g., {123}setting, {123}i1, {123}d1
	if cfg.MetadataRedisClient != nil {
		if err := cleanupRedisMetadata(ctx, cfg.MetadataRedisClient, cfg.RedisDB); err != nil {
			// Log but don't fail - best effort cleanup
			logger.L().Warn(ctx, "Failed to cleanup Redis metadata",
				zap.Error(err),
				zap.String("volume_id", cfg.VolumeID),
				zap.Int32("redis_db", cfg.RedisDB))
		} else {
			logger.L().Info(ctx, "Cleaned up Redis metadata",
				zap.String("volume_id", cfg.VolumeID),
				zap.Int32("redis_db", cfg.RedisDB))
		}
	}

	return nil
}

// cleanupRedisMetadata safely removes all JuiceFS metadata keys for a volume.
// Keys are stored with pattern {N}* where N is the Redis DB number.
// Uses SCAN to iterate (not KEYS) to avoid blocking in production.
func cleanupRedisMetadata(ctx context.Context, client redis.UniversalClient, redisDB int32) error {
	pattern := fmt.Sprintf("{%d}*", redisDB)
	cursor := uint64(0)
	totalDeleted := 0
	batchSize := int64(100)

	for {
		// SCAN with pattern to find matching keys
		keys, nextCursor, err := client.Scan(ctx, cursor, pattern, batchSize).Result()
		if err != nil {
			return fmt.Errorf("scan redis keys with pattern %s: %w", pattern, err)
		}

		// Delete found keys in batch
		if len(keys) > 0 {
			deleted, err := client.Del(ctx, keys...).Result()
			if err != nil {
				return fmt.Errorf("delete redis keys: %w", err)
			}
			totalDeleted += int(deleted)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if totalDeleted > 0 {
		logger.L().Debug(ctx, "Deleted Redis metadata keys",
			zap.Int("count", totalDeleted),
			zap.String("pattern", pattern))
	}

	return nil
}
