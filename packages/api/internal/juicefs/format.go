package juicefs

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
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

// FormatVolume creates the GCS bucket paths for a new volume.
// This creates marker files to establish the paths for JuiceFS data and Litestream metadata.
//
// JuiceFS metadata initialization is handled by envd during first mount:
// - litestream restore -if-replica-exists returns success for empty bucket
// - juicefs format creates fresh SQLite metadata
// - Litestream starts replicating to GCS
func FormatVolume(ctx context.Context, cfg FormatConfig) error {
	dataPrefix, metaPrefix := gcsPathsForVolume(cfg.PoolConfig.GCSBucket, cfg.VolumeID)

	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create GCS client: %w", err)
	}
	defer gcsClient.Close()

	bucket := gcsClient.Bucket(cfg.PoolConfig.GCSBucket)

	// Create marker files to establish bucket paths
	// GCS doesn't support empty folders, so we use .keep files
	markers := []string{
		dataPrefix + ".keep",
		metaPrefix + ".keep",
	}

	for _, marker := range markers {
		obj := bucket.Object(marker)
		writer := obj.NewWriter(ctx)
		if _, err := writer.Write([]byte{}); err != nil {
			writer.Close()
			return fmt.Errorf("write marker %s: %w", marker, err)
		}
		if err := writer.Close(); err != nil {
			return fmt.Errorf("close marker %s: %w", marker, err)
		}
	}

	logger.L().Info(ctx, "Volume paths created",
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
