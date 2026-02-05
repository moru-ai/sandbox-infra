// Package juicefs provides litestream restore and replicate functionality for volume metadata.
package juicefs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

const (
	// LitestreamBinary is the path to the litestream binary
	LitestreamBinary = "/usr/local/bin/litestream"

	// SQLite3Binary is the path to the sqlite3 binary
	SQLite3Binary = "/usr/bin/sqlite3"

	// RestoreTimeout is the maximum time to wait for litestream restore
	RestoreTimeout = 2 * time.Minute

	// ReplicateTimeout is the maximum time to wait for litestream replicate to sync
	ReplicateTimeout = 10 * time.Second

	// ReplicateSyncInterval is the sync interval for litestream replicate
	ReplicateSyncInterval = "100ms"
)

// RestoreResult contains the result of a litestream restore operation
type RestoreResult struct {
	// MetaDBPath is the path to the restored meta.db file
	MetaDBPath string

	// IsFreshVolume is true if no replica existed (fresh volume)
	IsFreshVolume bool
}

// restoreMetaDB restores the SQLite metadata DB from Litestream replica in GCS.
// For fresh volumes (no backup exists), this returns IsFreshVolume=true.
//
// The function creates a temp directory per volume at /tmp/juicefs-api/{volumeID}/
// and restores the meta.db there.
func restoreMetaDB(ctx context.Context, volumeID string, gcsBucket string) (*RestoreResult, error) {
	// Create temp directory for this volume
	tmpDir := filepath.Join("/tmp/juicefs-api", volumeID)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	metaDBPath := filepath.Join(tmpDir, "meta.db")
	replicaURL := fmt.Sprintf("gs://%s/%s-meta", gcsBucket, volumeID)

	ctx, cancel := context.WithTimeout(ctx, RestoreTimeout)
	defer cancel()

	// Clean up any existing meta.db from a previous attempt
	if err := os.Remove(metaDBPath); err != nil && !os.IsNotExist(err) {
		logger.L().Debug(ctx, "Failed to remove existing meta.db",
			zap.String("path", metaDBPath),
			zap.Error(err))
	}

	// litestream restore -if-replica-exists -o /tmp/juicefs-api/{volumeID}/meta.db gs://bucket/volumeID-meta
	cmd := exec.CommandContext(ctx, LitestreamBinary,
		"restore",
		"-if-replica-exists",
		"-o", metaDBPath,
		replicaURL,
	)

	// Use Application Default Credentials (ADC) - no token file needed for API server
	// The API server runs with a service account that has GCS access

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.L().Debug(ctx, "Running litestream restore",
		zap.String("volume_id", volumeID),
		zap.String("replica_url", replicaURL),
		zap.Strings("args", cmd.Args))

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("litestream restore failed: %w\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	// Check if meta.db was created (fresh volume if not)
	if _, err := os.Stat(metaDBPath); os.IsNotExist(err) {
		logger.L().Info(ctx, "Fresh volume - no existing replica",
			zap.String("volume_id", volumeID))
		return &RestoreResult{
			MetaDBPath:    metaDBPath,
			IsFreshVolume: true,
		}, nil
	}

	logger.L().Info(ctx, "Restored volume metadata",
		zap.String("volume_id", volumeID),
		zap.String("path", metaDBPath))

	return &RestoreResult{
		MetaDBPath:    metaDBPath,
		IsFreshVolume: false,
	}, nil
}

// convertJournalMode sets the SQLite journal mode to DELETE.
// This is required after litestream restore because JuiceFS cannot use WAL mode.
func convertJournalMode(ctx context.Context, metaDBPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, SQLite3Binary, metaDBPath, "PRAGMA journal_mode=DELETE;")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlite3 journal mode failed: %w\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	logger.L().Debug(ctx, "Converted journal mode to DELETE",
		zap.String("path", metaDBPath),
		zap.String("result", stdout.String()))

	return nil
}

// syncViaLitestream syncs the local meta.db back to GCS using litestream replicate.
// This runs litestream replicate briefly (with short sync-interval), waits for sync,
// then stops the process.
func syncViaLitestream(ctx context.Context, volumeID, metaDBPath, gcsBucket string) error {
	replicaURL := fmt.Sprintf("gs://%s/%s-meta", gcsBucket, volumeID)

	// Create a temporary litestream config file
	tmpDir := filepath.Dir(metaDBPath)
	configPath := filepath.Join(tmpDir, "litestream.yml")

	config := fmt.Sprintf(`dbs:
  - path: %s
    replicas:
      - url: %s
        sync-interval: %s
`, metaDBPath, replicaURL, ReplicateSyncInterval)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write litestream config: %w", err)
	}
	defer os.Remove(configPath)

	// Start litestream replicate
	cmd := exec.Command(LitestreamBinary, "replicate", "-config", configPath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	logger.L().Debug(ctx, "Starting litestream replicate for sync",
		zap.String("volume_id", volumeID),
		zap.String("config", configPath))

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start litestream replicate: %w", err)
	}

	// Wait for initial sync (give it time to sync changes)
	time.Sleep(ReplicateTimeout)

	// Stop litestream gracefully
	if cmd.Process != nil {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			// Process may have already exited, try kill
			cmd.Process.Kill()
		}
		// Wait for process to exit
		cmd.Wait()
	}

	logger.L().Info(ctx, "Synced metadata via litestream",
		zap.String("volume_id", volumeID))

	return nil
}

// cleanupVolumeDir removes the temp directory for a volume.
func cleanupVolumeDir(volumeID string) error {
	tmpDir := filepath.Join("/tmp/juicefs-api", volumeID)
	return os.RemoveAll(tmpDir)
}
