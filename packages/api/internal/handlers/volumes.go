package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/api/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/juicefs"
	"github.com/moru-ai/sandbox-infra/packages/db/queries"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/events"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/id"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

// volumeNamePattern validates volume names (slug format).
// - Must start with lowercase letter
// - Can contain lowercase letters, numbers, and hyphens
// - Must end with lowercase letter or number
// - Min 1 char, max 63 chars
var volumeNamePattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

const volumeIDPrefix = "vol_"

// PostVolumes creates a new volume (idempotent by name).
func (a *APIStore) PostVolumes(c *gin.Context) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	var req api.CreateVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate name
	if req.Name == "" {
		a.sendAPIStoreError(c, http.StatusBadRequest, "name is required")
		return
	}
	if !volumeNamePattern.MatchString(req.Name) {
		a.sendAPIStoreError(c, http.StatusBadRequest, "name must be lowercase alphanumeric with hyphens (1-63 chars)")
		return
	}

	// Check if volume with same name exists (idempotent)
	existing, err := a.sqlcDB.GetVolumeByName(ctx, queries.GetVolumeByNameParams{
		TeamID: team.ID,
		Name:   req.Name,
	})
	if err == nil {
		// Volume exists, return it (200 OK for idempotent)
		c.JSON(http.StatusOK, volumeToAPI(existing))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to check existing volume")
		return
	}

	// Generate volume ID
	volumeID := volumeIDPrefix + id.Generate()

	// Create volume record with status 'available'
	// Note: JuiceFS metadata initialization is handled by envd during first mount.
	// envd uses litestream restore -if-replica-exists which returns success for empty
	// buckets, then juicefs format creates fresh metadata. This eliminates the
	// API/litestream format mismatch that caused 401 errors.
	volume, err := a.sqlcDB.CreateVolume(ctx, queries.CreateVolumeParams{
		ID:     volumeID,
		TeamID: team.ID,
		Name:   req.Name,
		Status: "available",
	})
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to create volume")
		return
	}

	// Emit volume.created event
	if a.volEventsDelivery != nil {
		event := events.NewVolumeEvent(events.VolumeCreatedEvent, volumeID).
			WithVolumeName(req.Name)
		event.SandboxTeamID = team.ID

		go func() {
			if err := a.volEventsDelivery.Publish(context.WithoutCancel(ctx), events.DeliveryKey(team.ID), event); err != nil {
				logger.L().Error(ctx, "Failed to publish volume.created event", zap.Error(err), zap.String("volume_id", volumeID))
			}
		}()
	}
	logger.L().Info(ctx, "Volume created",
		zap.String("volume_id", volumeID),
		zap.String("volume_name", req.Name),
		zap.String("team_id", team.ID.String()),
	)

	c.JSON(http.StatusCreated, volumeToAPI(volume))
}

// GetVolumes lists all volumes for the authenticated team.
func (a *APIStore) GetVolumes(c *gin.Context, params api.GetVolumesParams) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	limit := int32(100)
	if params.Limit != nil && *params.Limit > 0 && *params.Limit <= 100 {
		limit = *params.Limit
	}

	volumes, err := a.sqlcDB.ListVolumes(ctx, queries.ListVolumesParams{
		TeamID:     team.ID,
		Status:     nil, // All statuses
		QueryLimit: limit,
	})
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to list volumes")
		return
	}

	result := make([]api.Volume, len(volumes))
	for i, v := range volumes {
		result[i] = volumeToAPI(v)
	}

	c.JSON(http.StatusOK, result)
}

// GetVolumesIdOrName gets a volume by ID or name.
func (a *APIStore) GetVolumesIdOrName(c *gin.Context, volumeID api.VolumeIdOrName) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	volume, err := a.resolveVolume(ctx, team.ID, volumeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.sendAPIStoreError(c, http.StatusNotFound, "Volume not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to get volume")
		return
	}

	c.JSON(http.StatusOK, volumeToAPI(volume))
}

// DeleteVolumesIdOrName deletes a volume by ID or name.
func (a *APIStore) DeleteVolumesIdOrName(c *gin.Context, volumeID api.VolumeIdOrName) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	volume, err := a.resolveVolume(ctx, team.ID, volumeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.sendAPIStoreError(c, http.StatusNotFound, "Volume not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to get volume")
		return
	}

	// Emit volume.deleted event
	if a.volEventsDelivery != nil {
		event := events.NewVolumeEvent(events.VolumeDeletedEvent, volume.ID).
			WithVolumeName(volume.Name)
		event.SandboxTeamID = team.ID

		go func() {
			if err := a.volEventsDelivery.Publish(context.WithoutCancel(ctx), events.DeliveryKey(team.ID), event); err != nil {
				logger.L().Error(ctx, "Failed to publish volume.deleted event", zap.Error(err), zap.String("volume_id", volume.ID))
			}
		}()
	}
	logger.L().Info(ctx, "Volume deletion started",
		zap.String("volume_id", volume.ID),
		zap.String("volume_name", volume.Name),
		zap.String("team_id", team.ID.String()),
	)

	// Mark as deleting
	_, err = a.sqlcDB.UpdateVolumeStatus(ctx, queries.UpdateVolumeStatusParams{
		ID:     volume.ID,
		Status: "deleting",
	})
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to update volume status")
		return
	}

	// Destroy JuiceFS volume (data + metadata in GCS)
	if a.volumesBucket != "" {
		destroyCfg := juicefs.FormatConfig{
			VolumeID: volume.ID,
			PoolConfig: juicefs.Config{
				GCSBucket: a.volumesBucket,
			},
		}
		// Best effort - don't fail if destroy fails
		if err := juicefs.DestroyVolume(ctx, destroyCfg, true); err != nil {
			logger.L().Warn(ctx, "Failed to destroy volume data",
				zap.Error(err),
				zap.String("volume_id", volume.ID))
		}
	}

	// Delete the record
	if err := a.sqlcDB.DeleteVolume(ctx, volume.ID); err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to delete volume")
		return
	}

	c.Status(http.StatusNoContent)
}

// resolveVolume looks up a volume by ID or name.
func (a *APIStore) resolveVolume(ctx context.Context, teamID uuid.UUID, idOrName string) (queries.Volume, error) {
	// If starts with vol_, lookup by ID
	if strings.HasPrefix(idOrName, volumeIDPrefix) {
		vol, err := a.sqlcDB.GetVolume(ctx, idOrName)
		if err != nil {
			return queries.Volume{}, err
		}
		// Verify team ownership
		if vol.TeamID != teamID {
			return queries.Volume{}, sql.ErrNoRows // Hide existence from other teams
		}
		return vol, nil
	}

	// Otherwise lookup by name
	return a.sqlcDB.GetVolumeByName(ctx, queries.GetVolumeByNameParams{
		TeamID: teamID,
		Name:   idOrName,
	})
}

// volumeToAPI converts a database volume to API response.
func volumeToAPI(v queries.Volume) api.Volume {
	vol := api.Volume{
		VolumeID:  v.ID,
		Name:      v.Name,
		CreatedAt: v.CreatedAt,
		UpdatedAt: v.UpdatedAt,
	}
	if v.TotalSizeBytes != nil {
		vol.TotalSizeBytes = v.TotalSizeBytes
	}
	if v.TotalFileCount != nil {
		vol.TotalFileCount = v.TotalFileCount
	}
	return vol
}
