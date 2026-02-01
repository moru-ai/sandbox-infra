package events

import (
	"time"

	"github.com/google/uuid"
)

// Volume lifecycle events
const (
	VolumeCreatedEvent  = "volume.created"
	VolumeDeletedEvent  = "volume.deleted"
	VolumeAttachedEvent = "volume.attached"
	VolumeDetachedEvent = "volume.detached"
)

// Volume mount events
const (
	VolumeMountStartedEvent   = "volume.mount.started"
	VolumeMountCompletedEvent = "volume.mount.completed"
	VolumeMountFailedEvent    = "volume.mount.failed"
)

// Sandbox shutdown volume unmount events
const (
	SandboxShutdownVolumeUnmountStartedEvent   = "sandbox.shutdown.volume_unmount.started"
	SandboxShutdownVolumeUnmountCompletedEvent = "sandbox.shutdown.volume_unmount.completed"
	SandboxShutdownVolumeUnmountFailedEvent    = "sandbox.shutdown.volume_unmount.failed"
)

// ValidVolumeEventTypes lists all valid volume event types
var ValidVolumeEventTypes = []string{
	VolumeCreatedEvent,
	VolumeDeletedEvent,
	VolumeAttachedEvent,
	VolumeDetachedEvent,
	VolumeMountStartedEvent,
	VolumeMountCompletedEvent,
	VolumeMountFailedEvent,
	SandboxShutdownVolumeUnmountStartedEvent,
	SandboxShutdownVolumeUnmountCompletedEvent,
	SandboxShutdownVolumeUnmountFailedEvent,
}

// VolumeEvent represents an analytics event for volume operations
type VolumeEvent struct {
	ID        uuid.UUID `json:"id"`
	Version   string    `json:"version"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`

	// Volume identification
	VolumeID   string `json:"volume_id"`
	VolumeName string `json:"volume_name,omitempty"`

	// Sandbox context (when applicable)
	SandboxID          string    `json:"sandbox_id,omitempty"`
	SandboxExecutionID string    `json:"sandbox_execution_id,omitempty"`
	SandboxTeamID      uuid.UUID `json:"sandbox_team_id,omitempty"`

	// Mount details (for mount events)
	MountPath string `json:"mount_path,omitempty"`

	// Error information (for failed events)
	ErrorMessage string `json:"error_message,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`

	// Additional event data
	EventData map[string]any `json:"event_data,omitempty"`
}

// NewVolumeEvent creates a new VolumeEvent with common fields initialized
func NewVolumeEvent(eventType string, volumeID string) VolumeEvent {
	return VolumeEvent{
		ID:        uuid.New(),
		Version:   StructureVersionV2,
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		VolumeID:  volumeID,
	}
}

// WithSandboxContext adds sandbox context to the event
func (e VolumeEvent) WithSandboxContext(sandboxID, executionID string, teamID uuid.UUID) VolumeEvent {
	e.SandboxID = sandboxID
	e.SandboxExecutionID = executionID
	e.SandboxTeamID = teamID
	return e
}

// WithMountPath adds mount path to the event
func (e VolumeEvent) WithMountPath(mountPath string) VolumeEvent {
	e.MountPath = mountPath
	return e
}

// WithError adds error information to the event
func (e VolumeEvent) WithError(message string, code string) VolumeEvent {
	e.ErrorMessage = message
	e.ErrorCode = code
	return e
}

// WithVolumeName adds volume name to the event
func (e VolumeEvent) WithVolumeName(name string) VolumeEvent {
	e.VolumeName = name
	return e
}

// WithEventData adds additional event data
func (e VolumeEvent) WithEventData(data map[string]any) VolumeEvent {
	e.EventData = data
	return e
}
