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

// createTestVolume creates a volume for testing, handling idempotent creates.
// Returns the created/existing volume.
func createTestVolume(t *testing.T, ctx context.Context, c *api.ClientWithResponses, name string) *api.Volume {
	t.Helper()

	// First try to delete any existing volume with this name
	_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, name, setup.WithAPIKey())

	resp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: name,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	// Get the volume from either 200 (existing) or 201 (created) response
	var volume *api.Volume
	if resp.JSON201 != nil {
		volume = resp.JSON201
	} else if resp.JSON200 != nil {
		volume = resp.JSON200
	}

	if volume == nil {
		t.Logf("Create response: %s", string(resp.Body))
	}
	require.NotNil(t, volume, "Expected volume in response, got status %d", resp.StatusCode())

	return volume
}

func TestVolumeCreate(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-create"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	assert.Equal(t, volumeName, volume.Name)
	assert.Contains(t, volume.VolumeID, "vol_")
}

func TestVolumeCreateIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-idempotent"
	volume1 := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume1.VolumeID, setup.WithAPIKey())
	})

	// Second create with same name should return existing (200 OK)
	resp2, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	// Should return 200 (existing) not 201 (created)
	assert.Equal(t, http.StatusOK, resp2.StatusCode())
	require.NotNil(t, resp2.JSON200)
	assert.Equal(t, volume1.VolumeID, resp2.JSON200.VolumeID)
}

func TestVolumeGetByID(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-get-by-id"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Get by ID
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, getResp.StatusCode())
	require.NotNil(t, getResp.JSON200)
	assert.Equal(t, volume.VolumeID, getResp.JSON200.VolumeID)
	assert.Equal(t, volumeName, getResp.JSON200.Name)
}

func TestVolumeGetByName(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-get-by-name"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// Get by name
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, volumeName, setup.WithAPIKey())
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, getResp.StatusCode())
	require.NotNil(t, getResp.JSON200)
	assert.Equal(t, volume.VolumeID, getResp.JSON200.VolumeID)
}

func TestVolumeList(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-list"
	volume := createTestVolume(t, ctx, c, volumeName)

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	})

	// List volumes
	listResp, err := c.GetVolumesWithResponse(ctx, &api.GetVolumesParams{}, setup.WithAPIKey())
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, listResp.StatusCode())
	require.NotNil(t, listResp.JSON200)

	// Should contain our created volume
	found := false
	for _, v := range *listResp.JSON200 {
		if v.VolumeID == volume.VolumeID {
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
	volume := createTestVolume(t, ctx, c, volumeName)

	// Delete the volume
	deleteResp, err := c.DeleteVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode())

	// Verify it's gone
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode())
}

func TestVolumeDeleteByName(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	volumeName := "test-volume-delete-name"
	volume := createTestVolume(t, ctx, c, volumeName)

	// Delete by name
	deleteResp, err := c.DeleteVolumesIdOrNameWithResponse(ctx, volumeName, setup.WithAPIKey())
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode())

	// Verify it's gone
	getResp, err := c.GetVolumesIdOrNameWithResponse(ctx, volume.VolumeID, setup.WithAPIKey())
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
