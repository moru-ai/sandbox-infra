package juicefs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/juicedata/juicefs/pkg/meta"
	"github.com/juicedata/juicefs/pkg/object"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

// FormatConfig holds configuration for formatting a new JuiceFS volume.
type FormatConfig struct {
	// VolumeID is the unique identifier for the volume (used in GCS path prefix)
	VolumeID string

	// PoolConfig contains the shared GCS configuration
	PoolConfig Config
}

// gcsPathsForVolume returns the GCS paths for a volume's data and metadata.
func gcsPathsForVolume(bucket, volumeID string) (dataPrefix, metaPrefix string) {
	dataPrefix = volumeID + "/"
	metaPrefix = volumeID + "-meta/"
	return
}

// FormatVolume initializes a new JuiceFS volume with SQLite metadata.
// This creates an empty SQLite metadata file and uploads it to GCS.
func FormatVolume(ctx context.Context, cfg FormatConfig) error {
	dataPrefix, metaPrefix := gcsPathsForVolume(cfg.PoolConfig.GCSBucket, cfg.VolumeID)

	// Create temporary directory for SQLite file
	tmpDir, err := os.MkdirTemp("", "juicefs-format-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	sqlitePath := filepath.Join(tmpDir, "meta.db")
	sqliteURL := "sqlite3://" + sqlitePath

	// Create metadata client with SQLite backend
	metaConf := meta.DefaultConf()
	metaConf.Retries = 10
	m := meta.NewClient(sqliteURL, metaConf)

	// Check if already formatted in GCS by checking for meta.db
	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create GCS client: %w", err)
	}
	defer gcsClient.Close()

	bucket := gcsClient.Bucket(cfg.PoolConfig.GCSBucket)
	metaObj := bucket.Object(metaPrefix + "meta.db")

	// Check if metadata already exists (idempotent)
	_, err = metaObj.Attrs(ctx)
	if err == nil {
		// Already formatted - this is idempotent, return success
		logger.L().Info(ctx, "Volume already formatted", zap.String("volume_id", cfg.VolumeID))
		return nil
	}
	if err != storage.ErrObjectNotExist {
		return fmt.Errorf("check existing metadata: %w", err)
	}

	// Create new format configuration
	bucketURL := fmt.Sprintf("gs://%s/", cfg.PoolConfig.GCSBucket)

	format := &meta.Format{
		Name:             cfg.VolumeID,
		UUID:             uuid.New().String(),
		Storage:          "gs",
		Bucket:           bucketURL + dataPrefix, // Data stored at {volumeID}/
		BlockSize:        4096,                   // 4 MiB blocks (in KiB units)
		Compression:      "lz4",
		TrashDays:        0, // Disable trash for API volumes
		DirStats:         true,
		MetaVersion:      meta.MaxVersion,
		MinClientVersion: "1.1.0-A",
	}

	// Initialize metadata (creates SQLite file)
	if err = m.Init(format, false); err != nil {
		return fmt.Errorf("init metadata: %w", err)
	}

	// Close metadata client to flush SQLite
	if err = m.Shutdown(); err != nil {
		logger.L().Warn(ctx, "Failed to shutdown metadata client", zap.Error(err))
	}

	// Upload SQLite file to GCS
	sqliteFile, err := os.Open(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite file: %w", err)
	}
	defer sqliteFile.Close()

	writer := metaObj.NewWriter(ctx)
	if _, err = io.Copy(writer, sqliteFile); err != nil {
		writer.Close()
		return fmt.Errorf("upload metadata to GCS: %w", err)
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("close GCS writer: %w", err)
	}

	// Write UUID marker to GCS data prefix
	baseBlob, err := object.CreateStorage("gs", bucketURL, "", "", "")
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	blob := object.WithPrefix(baseBlob, dataPrefix)

	if err = blob.Put(ctx, "juicefs_uuid", strings.NewReader(format.UUID)); err != nil {
		// Clean up metadata on failure
		_ = metaObj.Delete(ctx)
		return fmt.Errorf("write uuid marker: %w", err)
	}

	logger.L().Info(ctx, "Volume formatted successfully",
		zap.String("volume_id", cfg.VolumeID),
		zap.String("data_prefix", dataPrefix),
		zap.String("meta_prefix", metaPrefix))

	return nil
}

// DestroyVolume removes all JuiceFS data for a volume.
// This deletes both data objects and metadata from GCS.
func DestroyVolume(ctx context.Context, cfg FormatConfig, deleteData bool) error {
	if !deleteData {
		return nil
	}

	dataPrefix, metaPrefix := gcsPathsForVolume(cfg.PoolConfig.GCSBucket, cfg.VolumeID)

	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create GCS client: %w", err)
	}
	defer gcsClient.Close()

	bucket := gcsClient.Bucket(cfg.PoolConfig.GCSBucket)

	// Delete all objects under data prefix
	dataDeleted, err := deleteGCSPrefix(ctx, bucket, dataPrefix)
	if err != nil {
		logger.L().Warn(ctx, "Failed to delete volume data",
			zap.Error(err),
			zap.String("volume_id", cfg.VolumeID),
			zap.String("prefix", dataPrefix))
	} else {
		logger.L().Info(ctx, "Deleted volume data",
			zap.String("volume_id", cfg.VolumeID),
			zap.Int("objects_deleted", dataDeleted))
	}

	// Delete all objects under metadata prefix
	metaDeleted, err := deleteGCSPrefix(ctx, bucket, metaPrefix)
	if err != nil {
		logger.L().Warn(ctx, "Failed to delete volume metadata",
			zap.Error(err),
			zap.String("volume_id", cfg.VolumeID),
			zap.String("prefix", metaPrefix))
	} else {
		logger.L().Info(ctx, "Deleted volume metadata",
			zap.String("volume_id", cfg.VolumeID),
			zap.Int("objects_deleted", metaDeleted))
	}

	return nil
}

// deleteGCSPrefix deletes all objects under a prefix in GCS.
// Returns the number of objects deleted.
func deleteGCSPrefix(ctx context.Context, bucket *storage.BucketHandle, prefix string) (int, error) {
	deleted := 0

	it := bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return deleted, fmt.Errorf("list objects: %w", err)
		}

		if err := bucket.Object(attrs.Name).Delete(ctx); err != nil {
			// Log but continue - best effort deletion
			logger.L().Debug(ctx, "Failed to delete object",
				zap.String("object", attrs.Name),
				zap.Error(err))
			continue
		}
		deleted++
	}

	return deleted, nil
}
