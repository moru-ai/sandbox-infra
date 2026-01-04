package sandboxes

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moru-ai/sandbox-infra/tests/integration/internal/api"
	"github.com/moru-ai/sandbox-infra/tests/integration/internal/setup"
	"github.com/moru-ai/sandbox-infra/tests/integration/internal/utils"
)

func TestSandboxRuns_KilledEndReason(t *testing.T) {
	c := setup.GetAPIClient()
	db := setup.GetTestDBClient(t)

	t.Run("sandbox killed by user should have end_reason=killed", func(t *testing.T) {
		// Create a sandbox
		sbx := utils.SetupSandboxWithCleanup(t, c, utils.WithTimeout(300))
		sandboxID := sbx.SandboxID

		// Kill the sandbox
		killResp, err := c.DeleteSandboxesSandboxIDWithResponse(t.Context(), sandboxID, setup.WithAPIKey())
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, killResp.StatusCode())

		// Wait for the event to be processed by the consumer
		time.Sleep(2 * time.Second)

		// Query sandbox_runs table to verify end_reason
		var status, endReason string
		err = db.Pool().QueryRow(t.Context(), `
			SELECT status, COALESCE(end_reason, '')
			FROM sandbox_runs
			WHERE sandbox_id = $1
		`, sandboxID).Scan(&status, &endReason)

		require.NoError(t, err, "sandbox_runs record should exist for sandbox %s", sandboxID)
		assert.Equal(t, "stopped", status, "status should be 'stopped'")
		assert.Equal(t, "killed", endReason, "end_reason should be 'killed' for user-initiated kill")
	})
}

func TestSandboxRuns_TimeoutEndReason(t *testing.T) {
	c := setup.GetAPIClient()
	db := setup.GetTestDBClient(t)

	t.Run("sandbox timeout should have end_reason=timeout", func(t *testing.T) {
		// Create a sandbox with very short timeout (5 seconds)
		timeout := int32(5)
		createResp, err := c.PostSandboxesWithResponse(t.Context(), api.NewSandbox{
			TemplateID: setup.SandboxTemplateID,
			Timeout:    &timeout,
		}, setup.WithAPIKey())

		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, createResp.StatusCode())
		require.NotNil(t, createResp.JSON201)

		sandboxID := createResp.JSON201.SandboxID
		t.Logf("Created sandbox %s with 5 second timeout", sandboxID)

		// Wait for timeout + processing time
		t.Log("Waiting for sandbox to timeout...")
		time.Sleep(10 * time.Second)

		// Query sandbox_runs table to verify end_reason
		var status, endReason string
		err = db.Pool().QueryRow(t.Context(), `
			SELECT status, COALESCE(end_reason, '')
			FROM sandbox_runs
			WHERE sandbox_id = $1
		`, sandboxID).Scan(&status, &endReason)

		require.NoError(t, err, "sandbox_runs record should exist for sandbox %s", sandboxID)
		assert.Equal(t, "stopped", status, "status should be 'stopped'")
		assert.Equal(t, "timeout", endReason, "end_reason should be 'timeout' for timed-out sandbox")
	})
}

func TestSandboxRuns_PausedStatus(t *testing.T) {
	c := setup.GetAPIClient()
	db := setup.GetTestDBClient(t)

	t.Run("paused sandbox should have status=paused", func(t *testing.T) {
		// Create a sandbox
		sbx := utils.SetupSandboxWithCleanup(t, c, utils.WithTimeout(300))
		sandboxID := sbx.SandboxID

		// Pause the sandbox
		pauseResp, err := c.PostSandboxesSandboxIDPauseWithResponse(t.Context(), sandboxID, setup.WithAPIKey())
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, pauseResp.StatusCode())

		// Wait for the event to be processed
		time.Sleep(2 * time.Second)

		// Query sandbox_runs table to verify status
		var status string
		err = db.Pool().QueryRow(t.Context(), `
			SELECT status
			FROM sandbox_runs
			WHERE sandbox_id = $1
		`, sandboxID).Scan(&status)

		require.NoError(t, err, "sandbox_runs record should exist for sandbox %s", sandboxID)
		assert.Equal(t, "paused", status, "status should be 'paused'")

		// Cleanup - kill the paused sandbox
		_, _ = c.DeleteSandboxesSandboxIDWithResponse(t.Context(), sandboxID, setup.WithAPIKey())
	})
}

func TestSandboxRuns_Created(t *testing.T) {
	c := setup.GetAPIClient()
	db := setup.GetTestDBClient(t)

	t.Run("created sandbox should have a sandbox_runs record with status=running", func(t *testing.T) {
		// Create a sandbox
		sbx := utils.SetupSandboxWithCleanup(t, c, utils.WithTimeout(60))
		sandboxID := sbx.SandboxID

		// Wait for the event to be processed
		time.Sleep(2 * time.Second)

		// Query sandbox_runs table to verify it was created
		var status, templateID string
		err := db.Pool().QueryRow(t.Context(), `
			SELECT status, template_id
			FROM sandbox_runs
			WHERE sandbox_id = $1
		`, sandboxID).Scan(&status, &templateID)

		require.NoError(t, err, "sandbox_runs record should exist for sandbox %s", sandboxID)
		assert.Equal(t, "running", status, "status should be 'running'")
		assert.Equal(t, setup.SandboxTemplateID, templateID, "template_id should match")
	})
}
