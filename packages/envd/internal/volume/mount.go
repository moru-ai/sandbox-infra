// Package volume provides JuiceFS volume mounting functionality.
package volume

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/moru-ai/sandbox-infra/packages/envd/internal/host"
)

const (
	// JuiceFSBinary is the path to the JuiceFS binary.
	JuiceFSBinary = "/usr/local/bin/juicefs"

	// GCSProxyPort is the port where the GCS proxy listens.
	GCSProxyPort = 5017

	// RedisProxyPort is the port where the Redis proxy listens.
	RedisProxyPort = 5018

	// MountTimeout is the maximum time to wait for mount to complete.
	MountTimeout = 2 * time.Minute
)

// Mounter handles JuiceFS volume mounting.
type Mounter struct {
	config    *host.VolumeConfig
	mountPath string
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
	// Check if JuiceFS binary exists
	if _, err := os.Stat(JuiceFSBinary); os.IsNotExist(err) {
		return fmt.Errorf("JuiceFS binary not found at %s", JuiceFSBinary)
	}

	// Create mount directory if it doesn't exist
	if err := os.MkdirAll(m.mountPath, 0o755); err != nil {
		return fmt.Errorf("create mount directory: %w", err)
	}

	// Build Redis URL: redis://{proxyHost}:5018/{redisDb}
	redisURL := fmt.Sprintf("redis://%s:%d/%d",
		m.config.ProxyHost,
		RedisProxyPort,
		m.config.RedisDB,
	)

	// Set GCS endpoint environment variable
	gcsEndpoint := fmt.Sprintf("http://%s:%d",
		m.config.ProxyHost,
		GCSProxyPort,
	)

	// Build mount command
	// juicefs mount --no-usage-report --no-bgjob -d {redisUrl} {mountPath}
	ctx, cancel := context.WithTimeout(ctx, MountTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, JuiceFSBinary,
		"mount",
		"--no-usage-report",
		"--no-bgjob",
		"-d", // daemon mode
		redisURL,
		m.mountPath,
	)

	// Set environment variable for GCS endpoint
	cmd.Env = append(os.Environ(), "JFS_GCS_ENDPOINT="+gcsEndpoint)

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("juicefs mount failed: %w\nOutput: %s", err, string(output))
	}

	// Verify mount is accessible
	if err := m.verifyMount(); err != nil {
		return fmt.Errorf("mount verification failed: %w", err)
	}

	return nil
}

// Unmount unmounts the JuiceFS volume.
func (m *Mounter) Unmount(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, MountTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, JuiceFSBinary, "umount", m.mountPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("juicefs umount failed: %w\nOutput: %s", err, string(output))
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
