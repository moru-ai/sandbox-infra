// Package volume provides JuiceFS volume mounting functionality.
package volume

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/moru-ai/sandbox-infra/packages/envd/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/envd/internal/host"
)

func init() {
	// Register the volume mounter factory with the host package
	host.DefaultVolumeMounterFactory = func(config *host.VolumeConfig) host.VolumeMounter {
		return NewMounter(config)
	}

	// Register the volume unmounter factory with the api package for graceful shutdown
	api.DefaultVolumeUnmounterFactory = func(config *host.VolumeConfig) api.VolumeUnmounter {
		return NewMounter(config)
	}
}

const (
	// JuiceFSBinary is the path to the JuiceFS binary.
	JuiceFSBinary = "/usr/local/bin/juicefs"

	// LitestreamBinary is the path to the Litestream binary.
	LitestreamBinary = "/usr/local/bin/litestream"

	// SQLite3Binary is the path to the SQLite3 binary.
	SQLite3Binary = "/usr/bin/sqlite3"

	// GCSTokenFile is the path where the GCS token is written.
	GCSTokenFile = "/tmp/gcs-token"

	// MetaDBPath is the path for the SQLite metadata database.
	MetaDBPath = "/tmp/meta.db"

	// LitestreamConfigPath is the path for the Litestream configuration.
	LitestreamConfigPath = "/tmp/litestream.yml"

	// MountTimeout is the maximum time to wait for mount to complete.
	MountTimeout = 2 * time.Minute

	// LitestreamShutdownTimeout is the max time to wait for Litestream graceful shutdown.
	LitestreamShutdownTimeout = 10 * time.Second
)

// currentMounter holds the active mounter instance for graceful shutdown.
// This is needed because Unmount is called via a factory that creates a new instance,
// but we need access to the litestreamCmd from the original Mount call.
var currentMounter *Mounter

// Mounter handles JuiceFS volume mounting with SQLite + Litestream.
type Mounter struct {
	config        *host.VolumeConfig
	mountPath     string
	litestreamCmd *exec.Cmd // Track for graceful shutdown
}

// NewMounter creates a new volume mounter.
func NewMounter(config *host.VolumeConfig) *Mounter {
	return &Mounter{
		config:    config,
		mountPath: config.MountPath,
	}
}

// Mount mounts the JuiceFS volume at the configured path.
func (m *Mounter) Mount(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "[volume.mount.started] volume_id=%s mount_path=%s\n",
		m.config.VolumeID, m.mountPath)

	// Check if JuiceFS binary exists
	if _, err := os.Stat(JuiceFSBinary); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("JuiceFS binary not found at %s", JuiceFSBinary)
	}

	// Check if Litestream binary exists
	if _, err := os.Stat(LitestreamBinary); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("Litestream binary not found at %s", LitestreamBinary)
	}

	// Create mount directory if it doesn't exist
	if err := os.MkdirAll(m.mountPath, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("create mount directory: %w", err)
	}

	// Step 1: Write GCS token to file
	if err := m.writeGCSToken(); err != nil {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("write GCS token: %w", err)
	}

	// Step 2: Restore metadata database from Litestream (if replica exists)
	if err := m.restoreMetaDB(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("restore metadata DB: %w", err)
	}

	// Step 2b: For fresh volumes, format JuiceFS (creates meta.db)
	if _, err := os.Stat(MetaDBPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[volume.mount.fresh] volume_id=%s no existing backup, formatting new volume\n",
			m.config.VolumeID)
		if err := m.formatVolume(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
				m.config.VolumeID, m.mountPath, err)
			return fmt.Errorf("format volume: %w", err)
		}
	}

	// Step 3: Convert journal mode to DELETE (required after restore)
	if err := m.convertJournalMode(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("convert journal mode: %w", err)
	}

	// Step 4: Start Litestream replication daemon
	if err := m.startLitestream(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("start Litestream: %w", err)
	}

	// Step 5: Mount JuiceFS
	if err := m.mountJuiceFS(ctx); err != nil {
		// Cleanup Litestream on mount failure
		m.stopLitestream()
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("mount JuiceFS: %w", err)
	}

	// Verify mount is accessible
	if err := m.verifyMount(); err != nil {
		// Cleanup on verification failure
		m.stopLitestream()
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("mount verification failed: %w", err)
	}

	// Store the current mounter for graceful shutdown
	currentMounter = m

	fmt.Fprintf(os.Stderr, "[volume.mount.completed] volume_id=%s mount_path=%s\n",
		m.config.VolumeID, m.mountPath)

	return nil
}

// Unmount unmounts the JuiceFS volume and stops Litestream.
func (m *Mounter) Unmount(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, MountTimeout)
	defer cancel()

	// Step 1: Unmount JuiceFS
	cmd := exec.CommandContext(ctx, JuiceFSBinary, "umount", m.mountPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("juicefs umount failed: %w\nOutput: %s", err, string(output))
	}

	// Step 2: Checkpoint WAL to ensure all changes are in main DB file
	if err := m.checkpointWAL(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[volume.unmount.warning] WAL checkpoint failed: %v\n", err)
		// Continue with Litestream shutdown - it will still replicate the main DB
	}

	// Step 3: Stop Litestream gracefully
	// Use the currentMounter which has the litestreamCmd from Mount()
	if currentMounter != nil {
		if err := currentMounter.stopLitestream(); err != nil {
			return fmt.Errorf("stop Litestream: %w", err)
		}
		currentMounter = nil
	}

	return nil
}

// writeGCSToken writes the GCS access token to a file.
func (m *Mounter) writeGCSToken() error {
	if err := os.WriteFile(GCSTokenFile, []byte(m.config.GCSToken), 0o600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

// restoreMetaDB restores the SQLite metadata DB from Litestream replica.
// For fresh volumes (no backup exists), this is a no-op.
func (m *Mounter) restoreMetaDB(ctx context.Context) error {
	replicaURL := fmt.Sprintf("gs://%s/%s-meta", m.config.GCSBucket, m.config.VolumeID)

	ctx, cancel := context.WithTimeout(ctx, MountTimeout)
	defer cancel()

	// litestream restore -if-replica-exists -o /tmp/meta.db gs://bucket/volumeID-meta
	cmd := exec.CommandContext(ctx, LitestreamBinary,
		"restore",
		"-if-replica-exists",
		"-o", MetaDBPath,
		replicaURL,
	)

	cmd.Env = append(os.Environ(),
		"LITESTREAM_GCS_TOKEN_FILE="+GCSTokenFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("litestream restore failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Fprintf(os.Stderr, "[volume.mount.restore] volume_id=%s output=%s\n",
		m.config.VolumeID, string(output))

	return nil
}

// formatVolume initializes a fresh JuiceFS volume with SQLite metadata.
// This is called when no existing backup was restored (fresh volume).
func (m *Mounter) formatVolume(ctx context.Context) error {
	metaURL := fmt.Sprintf("sqlite3://%s", MetaDBPath)
	dataURL := fmt.Sprintf("gs://%s/%s", m.config.GCSBucket, m.config.VolumeID)

	// JuiceFS volume names only allow alphanumeric and hyphens (3-63 chars)
	// Replace underscores with hyphens
	volumeName := strings.ReplaceAll(m.config.VolumeID, "_", "-")

	ctx, cancel := context.WithTimeout(ctx, MountTimeout)
	defer cancel()

	// juicefs format --storage gs --bucket gs://bucket/volumeID sqlite3:///tmp/meta.db volumeID
	cmd := exec.CommandContext(ctx, JuiceFSBinary,
		"format",
		"--storage", "gs",
		"--bucket", dataURL,
		"--no-update",
		metaURL,
		volumeName, // volume name (sanitized)
	)

	cmd.Env = append(os.Environ(),
		"JFS_GCS_TOKEN_FILE="+GCSTokenFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("juicefs format failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Fprintf(os.Stderr, "[volume.mount.format] volume_id=%s output=%s\n",
		m.config.VolumeID, string(output))

	return nil
}

// convertJournalMode sets the SQLite journal mode to DELETE.
// This is required after Litestream restore because JuiceFS cannot use WAL mode.
func (m *Mounter) convertJournalMode(ctx context.Context) error {
	// Only convert if the database file exists (fresh volume won't have one)
	if _, err := os.Stat(MetaDBPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[volume.mount.journal] volume_id=%s skipping (no existing DB)\n",
			m.config.VolumeID)
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, SQLite3Binary, MetaDBPath, "PRAGMA journal_mode=DELETE;")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite3 journal mode failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Fprintf(os.Stderr, "[volume.mount.journal] volume_id=%s mode=%s\n",
		m.config.VolumeID, string(output))

	return nil
}

// startLitestream starts the Litestream replication daemon in the background.
func (m *Mounter) startLitestream(ctx context.Context) error {
	// Write Litestream config file
	if err := m.writeLitestreamConfig(); err != nil {
		return fmt.Errorf("write litestream config: %w", err)
	}

	// Start Litestream replicate daemon
	cmd := exec.Command(LitestreamBinary, "replicate", "-config", LitestreamConfigPath)
	cmd.Env = append(os.Environ(),
		"LITESTREAM_GCS_TOKEN_FILE="+GCSTokenFile,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start litestream: %w", err)
	}

	m.litestreamCmd = cmd

	fmt.Fprintf(os.Stderr, "[volume.mount.litestream] volume_id=%s pid=%d\n",
		m.config.VolumeID, cmd.Process.Pid)

	return nil
}

// writeLitestreamConfig writes the Litestream configuration file.
func (m *Mounter) writeLitestreamConfig() error {
	replicaURL := fmt.Sprintf("gs://%s/%s-meta", m.config.GCSBucket, m.config.VolumeID)

	config := fmt.Sprintf(`dbs:
  - path: %s
    replicas:
      - url: %s
        sync-interval: 1s
`, MetaDBPath, replicaURL)

	if err := os.WriteFile(LitestreamConfigPath, []byte(config), 0o644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// stopLitestream gracefully stops the Litestream daemon.
func (m *Mounter) stopLitestream() error {
	if m.litestreamCmd == nil || m.litestreamCmd.Process == nil {
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := m.litestreamCmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited
		if err.Error() != "os: process already finished" {
			fmt.Fprintf(os.Stderr, "[volume.unmount.litestream] SIGTERM failed: %v\n", err)
		}
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		_, err := m.litestreamCmd.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
		fmt.Fprintf(os.Stderr, "[volume.unmount.litestream] volume_id=%s stopped gracefully\n",
			m.config.VolumeID)
	case <-time.After(LitestreamShutdownTimeout):
		// Force kill if graceful shutdown takes too long
		fmt.Fprintf(os.Stderr, "[volume.unmount.litestream] volume_id=%s forcing kill after timeout\n",
			m.config.VolumeID)
		if err := m.litestreamCmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill litestream: %w", err)
		}
	}

	m.litestreamCmd = nil
	return nil
}

// checkpointWAL forces a WAL checkpoint to ensure all changes are in the main DB file.
func (m *Mounter) checkpointWAL(ctx context.Context) error {
	if _, err := os.Stat(MetaDBPath); os.IsNotExist(err) {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, SQLite3Binary, MetaDBPath, "PRAGMA wal_checkpoint(TRUNCATE);")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wal checkpoint failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Fprintf(os.Stderr, "[volume.unmount.checkpoint] volume_id=%s result=%s\n",
		m.config.VolumeID, string(output))

	return nil
}

// mountJuiceFS mounts the JuiceFS filesystem using SQLite metadata.
func (m *Mounter) mountJuiceFS(ctx context.Context) error {
	metaURL := fmt.Sprintf("sqlite3://%s", MetaDBPath)

	ctx, cancel := context.WithTimeout(ctx, MountTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, JuiceFSBinary,
		"mount",
		"--no-usage-report",
		"--no-bgjob",
		"-d",             // daemon mode
		"-o", "allow_other", // allow non-root users to access mount
		metaURL,
		m.mountPath,
	)

	// Set environment variables for JuiceFS
	cmd.Env = append(os.Environ(),
		"JFS_GCS_TOKEN_FILE="+GCSTokenFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("juicefs mount failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// verifyMount checks that the mount point is accessible.
func (m *Mounter) verifyMount() error {
	// Try to access the mount point
	entries, err := os.ReadDir(m.mountPath)
	if err != nil {
		return fmt.Errorf("read mount directory: %w", err)
	}

	// Mount is working if we can read the directory (even if empty)
	_ = entries

	return nil
}

// MountPath returns the mount path.
func (m *Mounter) MountPath() string {
	return m.mountPath
}

// IsMounted checks if the volume is currently mounted.
func (m *Mounter) IsMounted() bool {
	// Check if .juicefs hidden directory exists (created by JuiceFS)
	juicefsDir := filepath.Join(m.mountPath, ".juicefs")
	_, err := os.Stat(juicefsDir)
	return err == nil
}
