package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/moru-ai/sandbox-infra/packages/api/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/auth"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/db/types"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/utils"
	apiedge "github.com/moru-ai/sandbox-infra/packages/shared/pkg/http/edge"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/telemetry"
)

func (a *APIStore) GetSandboxesSandboxIDLogs(c *gin.Context, sandboxID string, params api.GetSandboxesSandboxIDLogsParams) {
	ctx := c.Request.Context()
	sandboxID = utils.ShortID(sandboxID)

	team := c.Value(auth.TeamContextKey).(*types.Team)

	telemetry.SetAttributes(ctx,
		attribute.String("instance.id", sandboxID),
		telemetry.WithTeamID(team.ID.String()),
	)

	// Sandboxes living in a cluster
	sbxLogs, err := a.getClusterSandboxLogs(ctx, sandboxID, team.ID.String(), utils.WithClusterFallback(team.ClusterID), params)
	if err != nil {
		a.sendAPIStoreError(c, int(err.Code), err.Message)

		return
	}

	c.JSON(http.StatusOK, sbxLogs)
}

func (a *APIStore) getClusterSandboxLogs(ctx context.Context, sandboxID string, teamID string, clusterID uuid.UUID, params api.GetSandboxesSandboxIDLogsParams) (*api.SandboxLogs, *api.Error) {
	cluster, ok := a.clustersPool.GetClusterById(clusterID)
	if !ok {
		telemetry.ReportCriticalError(ctx, "error getting cluster by ID", fmt.Errorf("cluster with ID '%s' not found", clusterID))

		return nil, &api.Error{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Error getting cluster '%s'", clusterID),
		}
	}

	edgeParams := &apiedge.V1SandboxLogsParams{
		TeamID: teamID,
		Cursor: params.Cursor,
		Limit:  params.Limit,
	}
	if params.Direction != nil {
		direction := apiedge.LogsDirection(*params.Direction)
		edgeParams.Direction = &direction
	}
	if params.EventType != nil {
		eventType := apiedge.SandboxLogEventType(*params.EventType)
		edgeParams.EventType = &eventType
	}

	res, err := cluster.GetHttpClient().V1SandboxLogsWithResponse(
		ctx, sandboxID, edgeParams,
	)
	if err != nil {
		telemetry.ReportCriticalError(ctx, "error when returning logs for sandbox", err)

		return nil, &api.Error{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Error returning logs for sandbox '%s'", sandboxID),
		}
	}

	if res.JSON200 == nil {
		telemetry.ReportCriticalError(ctx, "error when returning logs for sandbox", fmt.Errorf("unexpected response for sandbox '%s': %s", sandboxID, string(res.Body)))

		return nil, &api.Error{
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Error returning logs for sandbox '%s'", sandboxID),
		}
	}

	l := make([]api.SandboxLog, 0)
	for _, row := range res.JSON200.Logs {
		l = append(l, api.SandboxLog{Line: row.Line, Timestamp: row.Timestamp})
	}

	le := make([]api.SandboxLogEntry, 0)
	for _, row := range res.JSON200.LogEntries {
		le = append(
			le, api.SandboxLogEntry{
				Timestamp: row.Timestamp,
				EventType: api.SandboxLogEventType(row.EventType),
				Message:   row.Message,
				Fields:    row.Fields,
			},
		)
	}

	// Filter out system logs from template build (timestamps before sandbox creation).
	// This is safe for pause/resume: created_at is immutable and only set once when sandbox is first created.
	sandboxRun, err := a.sqlcDB.GetSandboxRun(ctx, sandboxID)
	if err == nil {
		filtered := make([]api.SandboxLogEntry, 0, len(le))
		for _, log := range le {
			if !log.Timestamp.Before(sandboxRun.CreatedAt) {
				filtered = append(filtered, log)
			}
		}
		le = filtered
	}

	return &api.SandboxLogs{Logs: l, LogEntries: le}, nil
}
