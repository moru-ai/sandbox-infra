package api

import (
	"context"
	"net/http"
	"time"

	"github.com/moru-ai/sandbox-infra/packages/envd/internal/host"
	"github.com/moru-ai/sandbox-infra/packages/envd/internal/logs"
)

const (
	// shutdownTimeout is the maximum time to wait for graceful shutdown operations.
	shutdownTimeout = 30 * time.Second
)

// VolumeUnmounter is the interface for unmounting JuiceFS volumes.
type VolumeUnmounter interface {
	Unmount(ctx context.Context) error
	MountPath() string
}

// VolumeUnmounterFactory creates a volume unmounter from config.
type VolumeUnmounterFactory func(config *host.VolumeConfig) VolumeUnmounter

// DefaultVolumeUnmounterFactory is set by the volume package during init.
var DefaultVolumeUnmounterFactory VolumeUnmounterFactory

// PostShutdown handles the POST /shutdown endpoint.
// This endpoint should be called before terminating the sandbox to ensure
// all data is flushed (e.g., JuiceFS has a 300MB write buffer).
func (a *API) PostShutdown(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	operationID := logs.AssignOperationID()
	logger := a.logger.With().Str(string(logs.OperationIDKey), operationID).Logger()

	logger.Info().Msg("Shutdown requested")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Unmount volumes if configured
	volumeConfig := host.CurrentVolumeConfig
	if volumeConfig != nil && DefaultVolumeUnmounterFactory != nil {
		// TODO: Emit sandbox.shutdown.volume_unmount.started analytics event when envd events delivery is added
		// Event should use events.SandboxShutdownVolumeUnmountStartedEvent type from shared/pkg/events/volume.go
		logger.Info().
			Str("volumeId", volumeConfig.VolumeID).
			Str("mountPath", volumeConfig.MountPath).
			Str("event", "sandbox.shutdown.volume_unmount.started").
			Msg("Unmounting volume for graceful shutdown")

		unmounter := DefaultVolumeUnmounterFactory(volumeConfig)
		if err := unmounter.Unmount(ctx); err != nil {
			// TODO: Emit sandbox.shutdown.volume_unmount.failed analytics event when envd events delivery is added
			// Event should use events.SandboxShutdownVolumeUnmountFailedEvent type from shared/pkg/events/volume.go
			logger.Error().
				Err(err).
				Str("volumeId", volumeConfig.VolumeID).
				Str("mountPath", volumeConfig.MountPath).
				Str("event", "sandbox.shutdown.volume_unmount.failed").
				Msg("Failed to unmount volume")
			jsonError(w, http.StatusInternalServerError, err)
			return
		}

		// TODO: Emit sandbox.shutdown.volume_unmount.completed analytics event when envd events delivery is added
		// Event should use events.SandboxShutdownVolumeUnmountCompletedEvent type from shared/pkg/events/volume.go
		logger.Info().
			Str("volumeId", volumeConfig.VolumeID).
			Str("mountPath", volumeConfig.MountPath).
			Str("event", "sandbox.shutdown.volume_unmount.completed").
			Msg("Volume unmounted successfully")
	} else {
		logger.Info().Msg("No volume to unmount")
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "")
	w.WriteHeader(http.StatusNoContent)
}
