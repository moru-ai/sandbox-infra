package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/api/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/auth"
	"github.com/moru-ai/sandbox-infra/packages/api/internal/db/types"
	"github.com/moru-ai/sandbox-infra/packages/db/queries"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/telemetry"
)

const (
	sandboxRunsDefaultLimit = int32(100)
	sandboxRunsMaxLimit     = int32(100)
)

func (a *APIStore) GetV2SandboxRuns(c *gin.Context, params api.GetV2SandboxRunsParams) {
	ctx := c.Request.Context()
	telemetry.ReportEvent(ctx, "list sandbox runs")

	teamInfo := c.Value(auth.TeamContextKey).(*types.Team)
	team := teamInfo.Team

	a.posthog.IdentifyAnalyticsTeam(ctx, team.ID.String(), team.Name)
	properties := a.posthog.GetPackageToPosthogProperties(&c.Request.Header)
	a.posthog.CreateAnalyticsTeamEvent(ctx, team.ID.String(), "listed sandbox runs", properties)

	// Parse limit
	limit := sandboxRunsDefaultLimit
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
		if limit > sandboxRunsMaxLimit {
			limit = sandboxRunsMaxLimit
		}
	}

	// Parse cursor time (default to now for first page)
	cursorTime := time.Now()
	if params.NextToken != nil && *params.NextToken != "" {
		parsedTime, err := time.Parse(time.RFC3339Nano, *params.NextToken)
		if err != nil {
			logger.L().Warn(ctx, "Invalid next token format", zap.Error(err))
			a.sendAPIStoreError(c, http.StatusBadRequest, "Invalid next token")
			return
		}
		cursorTime = parsedTime
	}

	// Parse status filter
	var statusFilter []string
	if params.Status != nil && len(*params.Status) > 0 {
		for _, s := range *params.Status {
			statusFilter = append(statusFilter, string(s))
		}
	}

	// Query database
	rows, err := a.sqlcDB.ListSandboxRuns(ctx, queries.ListSandboxRunsParams{
		TeamID:     team.ID,
		Status:     statusFilter,
		CursorTime: cursorTime,
		QueryLimit: limit + 1, // +1 to detect if there are more results
	})
	if err != nil {
		logger.L().Error(ctx, "Error listing sandbox runs", zap.Error(err))
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Error listing sandbox runs")
		return
	}

	// Check if there are more results
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}

	// Convert to API response
	runs := make([]api.SandboxRun, 0, len(rows))
	for _, row := range rows {
		run := api.SandboxRun{
			SandboxID:  row.SandboxID,
			TemplateID: row.TemplateID,
			Alias:      row.Alias,
			Status:     api.SandboxRunStatus(row.Status),
			CreatedAt:  row.CreatedAt,
		}

		if row.EndReason != nil {
			endReason := api.SandboxRunEndReason(*row.EndReason)
			run.EndReason = &endReason
		}

		if row.EndedAt != nil {
			run.EndedAt = row.EndedAt
		}

		runs = append(runs, run)
	}

	// Set next token header if there are more results
	if hasMore && len(runs) > 0 {
		lastRun := runs[len(runs)-1]
		c.Header("x-next-token", lastRun.CreatedAt.Format(time.RFC3339Nano))
	}

	c.JSON(http.StatusOK, runs)
}
