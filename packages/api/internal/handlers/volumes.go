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

	"github.com/moru-ai/sandbox-infra/packages/api/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/juicefs"
	"github.com/moru-ai/sandbox-infra/packages/db/queries"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/id"
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

	// Allocate Redis DB number
	redisDB, err := a.sqlcDB.AllocateRedisDB(ctx)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to allocate Redis DB")
		return
	}

	// Generate volume ID
	volumeID := volumeIDPrefix + id.Generate()

	// Generate secure random password (placeholder - needs crypto/rand)
	// For now using a placeholder - this should be properly implemented
	password := []byte("placeholder-password-needs-implementation")

	// Create volume record
	volume, err := a.sqlcDB.CreateVolume(ctx, queries.CreateVolumeParams{
		ID:                     volumeID,
		TeamID:                 team.ID,
		Name:                   req.Name,
		Status:                 "creating",
		RedisDb:                redisDB,
		RedisPasswordEncrypted: password, // TODO: encrypt properly
	})
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to create volume")
		return
	}

	// Format the JuiceFS volume if pool is configured
	if a.juicefsPool != nil {
		formatCfg := juicefs.FormatConfig{
			VolumeID:   volumeID,
			RedisDB:    redisDB,
			PoolConfig: a.juicefsPool.Config(),
		}
		if err := juicefs.FormatVolume(ctx, formatCfg); err != nil {
			// Mark volume as failed
			_, _ = a.sqlcDB.UpdateVolumeStatus(ctx, queries.UpdateVolumeStatusParams{
				ID:     volumeID,
				Status: "failed",
			})
			a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to format volume: "+err.Error())
			return
		}
	}

	// Update status to available
	volume, err = a.sqlcDB.UpdateVolumeStatus(ctx, queries.UpdateVolumeStatusParams{
		ID:     volumeID,
		Status: "available",
	})
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to update volume status")
		return
	}

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

	// Mark as deleting
	_, err = a.sqlcDB.UpdateVolumeStatus(ctx, queries.UpdateVolumeStatusParams{
		ID:     volume.ID,
		Status: "deleting",
	})
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to update volume status")
		return
	}

	// Destroy JuiceFS volume if pool is configured
	if a.juicefsPool != nil {
		destroyCfg := juicefs.FormatConfig{
			VolumeID:   volume.ID,
			RedisDB:    volume.RedisDb,
			PoolConfig: a.juicefsPool.Config(),
		}
		// Best effort - don't fail if destroy fails
		_ = juicefs.DestroyVolume(ctx, destroyCfg, true)
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
