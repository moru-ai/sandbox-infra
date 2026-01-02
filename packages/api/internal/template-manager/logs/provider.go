package logs

import (
	"context"
	"time"

	"github.com/moru-ai/sandbox-infra/packages/api/internal/api"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logs"
)

type Provider interface {
	GetLogs(ctx context.Context, templateID string, buildID string, offset int32, limit int32, level *logs.LogLevel, start time.Time, end time.Time, direction api.LogsDirection) ([]logs.LogEntry, error)
}
