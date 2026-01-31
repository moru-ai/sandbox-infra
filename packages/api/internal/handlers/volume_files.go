package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/moru-ai/sandbox-infra/packages/api/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/db/queries"
)

// GetVolumesVolumeIDFiles lists files in a volume.
func (a *APIStore) GetVolumesVolumeIDFiles(c *gin.Context, volumeID string, params api.GetVolumesVolumeIDFilesParams) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	// Verify volume ownership
	volume, err := a.resolveVolumeByID(ctx, team.ID, volumeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.sendAPIStoreError(c, http.StatusNotFound, "Volume not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to get volume")
		return
	}

	// Get path parameter
	path := "/"
	if params.Path != nil {
		path = *params.Path
	}

	// Validate path
	if !strings.HasPrefix(path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// TODO: Implement JuiceFS file listing
	// For now, return empty list
	_ = volume
	c.JSON(http.StatusOK, api.FileListResponse{
		Files: []api.FileInfo{},
	})
}

// GetVolumesVolumeIDFilesDownload streams file content from a volume.
func (a *APIStore) GetVolumesVolumeIDFilesDownload(c *gin.Context, volumeID string, params api.GetVolumesVolumeIDFilesDownloadParams) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	// Verify volume ownership
	volume, err := a.resolveVolumeByID(ctx, team.ID, volumeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.sendAPIStoreError(c, http.StatusNotFound, "Volume not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to get volume")
		return
	}

	// Validate path
	if !strings.HasPrefix(params.Path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// TODO: Implement JuiceFS file download streaming
	_ = volume
	a.sendAPIStoreError(c, http.StatusNotImplemented, "File download not yet implemented")
}

// PutVolumesVolumeIDFilesUpload streams file content to a volume.
func (a *APIStore) PutVolumesVolumeIDFilesUpload(c *gin.Context, volumeID string, params api.PutVolumesVolumeIDFilesUploadParams) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	// Verify volume ownership
	volume, err := a.resolveVolumeByID(ctx, team.ID, volumeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.sendAPIStoreError(c, http.StatusNotFound, "Volume not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to get volume")
		return
	}

	// Validate path
	if !strings.HasPrefix(params.Path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// TODO: Implement JuiceFS file upload streaming
	_ = volume
	a.sendAPIStoreError(c, http.StatusNotImplemented, "File upload not yet implemented")
}

// DeleteVolumesVolumeIDFiles deletes a file or directory from a volume.
func (a *APIStore) DeleteVolumesVolumeIDFiles(c *gin.Context, volumeID string, params api.DeleteVolumesVolumeIDFilesParams) {
	ctx := c.Request.Context()

	team, apiErr := a.GetTeam(ctx, c, nil)
	if apiErr != nil {
		a.sendAPIStoreError(c, apiErr.Code, apiErr.ClientMsg)
		return
	}

	// Verify volume ownership
	volume, err := a.resolveVolumeByID(ctx, team.ID, volumeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.sendAPIStoreError(c, http.StatusNotFound, "Volume not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to get volume")
		return
	}

	// Validate path
	if !strings.HasPrefix(params.Path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// TODO: Implement JuiceFS file deletion
	_ = volume
	a.sendAPIStoreError(c, http.StatusNotImplemented, "File deletion not yet implemented")
}

// resolveVolumeByID looks up a volume by ID only.
func (a *APIStore) resolveVolumeByID(ctx context.Context, teamID uuid.UUID, volumeID string) (queries.Volume, error) {
	// Volume ID must start with vol_
	if !strings.HasPrefix(volumeID, volumeIDPrefix) {
		return queries.Volume{}, sql.ErrNoRows
	}

	vol, err := a.sqlcDB.GetVolume(ctx, volumeID)
	if err != nil {
		return queries.Volume{}, err
	}
	// Verify team ownership
	if vol.TeamID != teamID {
		return queries.Volume{}, sql.ErrNoRows // Hide existence from other teams
	}
	return vol, nil
}
