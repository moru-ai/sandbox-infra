package volumes

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moru-ai/sandbox-infra/tests/integration/internal/api"
	"github.com/moru-ai/sandbox-infra/tests/integration/internal/setup"
)

func TestVolumeCreate(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-create"
	resp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Response: %s", string(resp.Body))
		}

		if resp.JSON201 != nil {
			// Clean up volume
			_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, resp.JSON201.VolumeID, setup.WithAPIKey())
		}
	})

	assert.Equal(t, http.StatusCreated, resp.StatusCode())
	require.NotNil(t, resp.JSON201)
	assert.Equal(t, volumeName, resp.JSON201.Name)
	assert.Contains(t, resp.JSON201.VolumeID, "vol_")
}

func TestVolumeCreateIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-idempotent"

	// First create
	resp1, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp1.StatusCode())
	require.NotNil(t, resp1.JSON201)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, resp1.JSON201.VolumeID, setup.WithAPIKey())
	})

	// Second create with same name should return existing (200 OK)
	resp2, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	// Should return 200 (existing) not 201 (created)
	assert.Equal(t, http.StatusOK, resp2.StatusCode())
	require.NotNil(t, resp2.JSON200)
	assert.Equal(t, resp1.JSON201.VolumeID, resp2.JSON200.VolumeID)
}

func TestVolumeGetByID(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-get-by-id"
	createResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createResp.StatusCode())
	require.NotNil(t, createResp.JSON201)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	})

	// Get by ID
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, getResp.StatusCode())
	require.NotNil(t, getResp.JSON200)
	assert.Equal(t, createResp.JSON201.VolumeID, getResp.JSON200.VolumeID)
	assert.Equal(t, volumeName, getResp.JSON200.Name)
}

func TestVolumeGetByName(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-get-by-name"
	createResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createResp.StatusCode())
	require.NotNil(t, createResp.JSON201)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	})

	// Get by name
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, volumeName, setup.WithAPIKey())
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, getResp.StatusCode())
	require.NotNil(t, getResp.JSON200)
	assert.Equal(t, createResp.JSON201.VolumeID, getResp.JSON200.VolumeID)
}

func TestVolumeList(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-list"
	createResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createResp.StatusCode())
	require.NotNil(t, createResp.JSON201)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	})

	// List volumes
	listResp, err := c.GetVolumesWithResponse(ctx, &api.GetVolumesParams{}, setup.WithAPIKey())
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, listResp.StatusCode())
	require.NotNil(t, listResp.JSON200)

	// Should contain our created volume
	found := false
	for _, v := range *listResp.JSON200 {
		if v.VolumeID == createResp.JSON201.VolumeID {
			found = true
			break
		}
	}
	assert.True(t, found, "Created volume should be in list")
}

func TestVolumeDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-delete"
	createResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createResp.StatusCode())
	require.NotNil(t, createResp.JSON201)

	// Delete the volume
	deleteResp, err := c.DeleteVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode())

	// Verify it's gone
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode())
}

func TestVolumeDeleteByName(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-delete-name"
	createResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createResp.StatusCode())
	require.NotNil(t, createResp.JSON201)

	// Delete by name
	deleteResp, err := c.DeleteVolumesIdOrNameWithResponse(ctx, volumeName, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode())

	// Verify it's gone
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, createResp.JSON201.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode())
}

func TestVolumeNotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Get non-existent volume by ID
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, "vol_nonexistent123", setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode())

	// Get non-existent volume by name
	getResp2, err := c.GetVolumesIdOrNameWithResponse(ctx, "nonexistent-volume", setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, getResp2.StatusCode())
}

func TestVolumeNameValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Invalid name: starts with number
	resp1, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: "123-invalid",
	}, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp1.StatusCode())

	// Invalid name: contains uppercase
	resp2, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: "Invalid-Name",
	}, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp2.StatusCode())

	// Invalid name: empty
	resp3, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: "",
	}, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp3.StatusCode())
}
