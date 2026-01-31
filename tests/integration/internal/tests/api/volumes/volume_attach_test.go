package volumes

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moru-ai/sandbox-infra/tests/integration/internal/api"
	"github.com/moru-ai/sandbox-infra/tests/integration/internal/setup"
	"github.com/moru-ai/sandbox-infra/tests/integration/internal/utils"
)

func TestSandboxWithVolume(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-sandbox-volume"
	volResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, volResp.StatusCode())
	require.NotNil(t, volResp.JSON201)

	volumeID := volResp.JSON201.VolumeID

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volumeID, setup.WithAPIKey())
	})

	// Create sandbox with volume attached
	sbxTimeout := int32(60)
	mountPath := "/workspace/data"
	sbxResp, err := c.PostSandboxesWithResponse(ctx, api.NewSandbox{
		TemplateID:      setup.SandboxTemplateID,
		Timeout:         &sbxTimeout,
		VolumeId:        &volumeID,
		VolumeMountPath: &mountPath,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Response: %s", string(sbxResp.Body))
		}

		if sbxResp.JSON201 != nil {
			utils.TeardownSandbox(t, c, sbxResp.JSON201.SandboxID)
		}
	})

	assert.Equal(t, http.StatusCreated, sbxResp.StatusCode())
	require.NotNil(t, sbxResp.JSON201)
}

func TestSandboxVolumeInvalidMountPath(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-sandbox-invalid-mount"
	volResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, volResp.StatusCode())
	require.NotNil(t, volResp.JSON201)

	volumeID := volResp.JSON201.VolumeID

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volumeID, setup.WithAPIKey())
	})

	// Try to create sandbox with invalid mount path
	sbxTimeout := int32(60)
	invalidMountPath := "/etc/passwd" // Not an allowed prefix
	sbxResp, err := c.PostSandboxesWithResponse(ctx, api.NewSandbox{
		TemplateID:      setup.SandboxTemplateID,
		Timeout:         &sbxTimeout,
		VolumeId:        &volumeID,
		VolumeMountPath: &invalidMountPath,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	// Should be rejected
	assert.Equal(t, http.StatusBadRequest, sbxResp.StatusCode())
}

func TestSandboxVolumeMissingMountPath(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Create a volume
	volumeName := "test-sandbox-missing-mount"
	volResp, err := c.PostVolumesWithResponse(ctx, api.CreateVolumeRequest{
		Name: volumeName,
	}, setup.WithAPIKey())
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, volResp.StatusCode())
	require.NotNil(t, volResp.JSON201)

	volumeID := volResp.JSON201.VolumeID

	t.Cleanup(func() {
		_, _ = c.DeleteVolumesIdOrNameWithResponse(ctx, volumeID, setup.WithAPIKey())
	})

	// Try to create sandbox with volume but no mount path
	sbxTimeout := int32(60)
	sbxResp, err := c.PostSandboxesWithResponse(ctx, api.NewSandbox{
		TemplateID: setup.SandboxTemplateID,
		Timeout:    &sbxTimeout,
		VolumeId:   &volumeID,
		// Missing VolumeMountPath
	}, setup.WithAPIKey())
	require.NoError(t, err)

	// Should be rejected
	assert.Equal(t, http.StatusBadRequest, sbxResp.StatusCode())
}

func TestSandboxVolumeNotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := setup.GetAPIClient()

	// Try to create sandbox with non-existent volume
	sbxTimeout := int32(60)
	nonExistentVolumeID := "vol_nonexistent123"
	mountPath := "/workspace/data"
	sbxResp, err := c.PostSandboxesWithResponse(ctx, api.NewSandbox{
		TemplateID:      setup.SandboxTemplateID,
		Timeout:         &sbxTimeout,
		VolumeId:        &nonExistentVolumeID,
		VolumeMountPath: &mountPath,
	}, setup.WithAPIKey())
	require.NoError(t, err)

	// Should be rejected with not found
	assert.Equal(t, http.StatusNotFound, sbxResp.StatusCode())
}
