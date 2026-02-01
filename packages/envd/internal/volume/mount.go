// Package volume provides JuiceFS volume mounting functionality.
package volume

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// TODO: Emit volume.mount.started analytics event when envd events delivery is added
	// Event should use events.VolumeMountStartedEvent type from shared/pkg/events/volume.go
	fmt.Fprintf(os.Stderr, "[volume.mount.started] volume_id=%s mount_path=%s\n",
		m.config.VolumeID, m.mountPath)

	// Check if JuiceFS binary exists
	if _, err := os.Stat(JuiceFSBinary); os.IsNotExist(err) {
		// TODO: Emit volume.mount.failed analytics event when envd events delivery is added
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("JuiceFS binary not found at %s", JuiceFSBinary)
	}

	// Create mount directory if it doesn't exist
	if err := os.MkdirAll(m.mountPath, 0o755); err != nil {
		// TODO: Emit volume.mount.failed analytics event when envd events delivery is added
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
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

	// Set environment variables for JuiceFS
	cmd.Env = append(os.Environ(),
		"JFS_GCS_ENDPOINT="+gcsEndpoint,
		"JFS_REDIS_NO_CLUSTER=1", // Disable cluster mode - proxy handles cluster communication
	)

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		// TODO: Emit volume.mount.failed analytics event when envd events delivery is added
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v output=%s\n",
			m.config.VolumeID, m.mountPath, err, string(output))
		return fmt.Errorf("juicefs mount failed: %w\nOutput: %s", err, string(output))
	}

	// Verify mount is accessible
	if err := m.verifyMount(); err != nil {
		// TODO: Emit volume.mount.failed analytics event when envd events delivery is added
		fmt.Fprintf(os.Stderr, "[volume.mount.failed] volume_id=%s mount_path=%s error=%v\n",
			m.config.VolumeID, m.mountPath, err)
		return fmt.Errorf("mount verification failed: %w", err)
	}

	// TODO: Emit volume.mount.completed analytics event when envd events delivery is added
	fmt.Fprintf(os.Stderr, "[volume.mount.completed] volume_id=%s mount_path=%s\n",
		m.config.VolumeID, m.mountPath)

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
