package logger_provider

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/moru-ai/sandbox-infra/packages/proxy/internal/cfg"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logs"
)

type LogsQueryProvider interface {
	QueryBuildLogs(ctx context.Context, templateID string, buildID string, start time.Time, end time.Time, limit int, offset int32, level *logs.LogLevel, direction logproto.Direction) ([]logs.LogEntry, error)
	// QuerySandboxLogs fetches sandbox logs.
	// eventType filters by specific event type ("stdout", "stderr", or "" for all).
	// If includeSystemLogs is true, returns all logs including process_start, process_end events.
	// If false, returns only stdout/stderr (user program output).
	QuerySandboxLogs(ctx context.Context, teamID string, sandboxID string, start time.Time, end time.Time, limit int, eventType string, direction logproto.Direction, includeSystemLogs bool) ([]logs.LogEntry, error)
}

func GetLogsQueryProvider(config cfg.Config) (LogsQueryProvider, error) {
	return NewLokiQueryProvider(config)
}
