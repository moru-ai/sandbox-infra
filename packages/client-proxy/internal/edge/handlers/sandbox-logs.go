package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/grafana/loki/pkg/logproto"

	api "github.com/moru-ai/sandbox-infra/packages/shared/pkg/http/edge"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logs"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/telemetry"
)

const (
	sandboxLogsOldestLimit = 168 * time.Hour // 7 days
	sandboxLogsLimit       = 100

	sandboxDefaultDirection = logproto.FORWARD
)

func (a *APIStore) V1SandboxLogs(c *gin.Context, sandboxID string, params api.V1SandboxLogsParams) {
	ctx := c.Request.Context()

	_, templateSpan := tracer.Start(c, "sandbox-logs-handler")
	defer templateSpan.End()

	direction := sandboxDefaultDirection
	if params.Direction != nil && *params.Direction == api.LogsDirectionBackward {
		direction = logproto.BACKWARD
	}

	end := time.Now()
	var start time.Time

	if params.Cursor != nil {
		start = time.UnixMilli(*params.Cursor)
	} else {
		start = end.Add(-sandboxLogsOldestLimit)
	}

	limit := sandboxLogsLimit
	if params.Limit != nil && *params.Limit <= sandboxLogsLimit {
		limit = int(*params.Limit)
	}

	logsRaw, err := a.queryLogsProvider.QuerySandboxLogs(ctx, params.TeamID, sandboxID, start, end, limit, apiLevelToLogLevel(params.Level), direction)
	if err != nil {
		a.sendAPIStoreError(c, http.StatusInternalServerError, "Error when fetching sandbox logs")
		telemetry.ReportCriticalError(ctx, "error when fetching sandbox logs", err)

		return
	}

	l := make([]api.SandboxLog, 0, len(logsRaw))
	le := make([]api.SandboxLogEntry, 0, len(logsRaw))

	for _, log := range logsRaw {
		l = append(l, api.SandboxLog{Timestamp: log.Timestamp, Line: log.Raw})
		le = append(
			le, api.SandboxLogEntry{
				Timestamp: log.Timestamp,
				Message:   log.Message,
				Level:     api.LogLevel(logs.LevelToString(log.Level)),
				Fields:    log.Fields,
			},
		)
	}

	c.JSON(http.StatusOK, api.SandboxLogsResponse{Logs: l, LogEntries: le})
}
