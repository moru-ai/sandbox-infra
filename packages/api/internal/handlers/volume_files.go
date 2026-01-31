package handlers

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	// Check if JuiceFS pool is configured
	if a.juicefsPool == nil {
		a.sendAPIStoreError(c, http.StatusServiceUnavailable, "Volume file operations not available")
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

	// Normalize path
	path = filepath.Clean(path)

	// Get JuiceFS client for this volume
	client, err := a.juicefsPool.Get(ctx, volume.ID, volume.RedisDb)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to connect to volume: "+err.Error())
		return
	}

	// List directory
	files, err := client.ListDir(ctx, path)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			a.sendAPIStoreError(c, http.StatusNotFound, "Path not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to list files: "+err.Error())
		return
	}

	// Convert to API response
	apiFiles := make([]api.FileInfo, 0, len(files))
	for _, f := range files {
		apiFile := api.FileInfo{
			Name:       f.Name,
			Path:       f.Path,
			Type:       api.FileInfoType(f.Type),
			ModifiedAt: ptr(f.ModifiedAt),
		}
		if f.Type == "file" {
			apiFile.Size = ptr(f.Size)
		}
		apiFiles = append(apiFiles, apiFile)
	}

	c.JSON(http.StatusOK, api.FileListResponse{
		Files: apiFiles,
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

	// Check if JuiceFS pool is configured
	if a.juicefsPool == nil {
		a.sendAPIStoreError(c, http.StatusServiceUnavailable, "Volume file operations not available")
		return
	}

	// Validate path
	if !strings.HasPrefix(params.Path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// Normalize path
	path := filepath.Clean(params.Path)

	// Get JuiceFS client for this volume
	client, err := a.juicefsPool.Get(ctx, volume.ID, volume.RedisDb)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to connect to volume: "+err.Error())
		return
	}

	// Download file
	reader, size, err := client.Download(ctx, path)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			a.sendAPIStoreError(c, http.StatusNotFound, "File not found")
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to download file: "+err.Error())
		return
	}
	defer reader.Close()

	// Set response headers
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")

	// Stream content
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
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

	// Check if JuiceFS pool is configured
	if a.juicefsPool == nil {
		a.sendAPIStoreError(c, http.StatusServiceUnavailable, "Volume file operations not available")
		return
	}

	// Validate path
	if !strings.HasPrefix(params.Path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// Normalize path
	path := filepath.Clean(params.Path)

	// Get JuiceFS client for this volume
	client, err := a.juicefsPool.Get(ctx, volume.ID, volume.RedisDb)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to connect to volume: "+err.Error())
		return
	}

	// Upload file
	written, err := client.Upload(ctx, path, c.Request.Body)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to upload file: "+err.Error())
		return
	}

	c.JSON(http.StatusCreated, api.UploadResponse{
		Path: path,
		Size: written,
	})
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

	// Check if JuiceFS pool is configured
	if a.juicefsPool == nil {
		a.sendAPIStoreError(c, http.StatusServiceUnavailable, "Volume file operations not available")
		return
	}

	// Validate path
	if !strings.HasPrefix(params.Path, "/") {
		a.sendAPIStoreError(c, http.StatusBadRequest, "Path must be absolute")
		return
	}

	// Normalize path
	path := filepath.Clean(params.Path)

	// Get recursive param
	recursive := false
	if params.Recursive != nil {
		recursive = *params.Recursive
	}

	// Get JuiceFS client for this volume
	client, err := a.juicefsPool.Get(ctx, volume.ID, volume.RedisDb)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to connect to volume: "+err.Error())
		return
	}

	// Delete file/directory
	err = client.Delete(ctx, path, recursive)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// Already deleted or doesn't exist - that's fine
			c.Status(http.StatusNoContent)
			return
		}
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Failed to delete: "+err.Error())
		return
	}

	c.Status(http.StatusNoContent)
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

// ptr returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}

// Ensure time.Time is used
var _ = time.Time{}
