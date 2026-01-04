package logger_provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	loki "github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logproto"
	"go.uber.org/zap"

	"github.com/moru-ai/sandbox-infra/packages/proxy/internal/cfg"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logs"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logs/logsloki"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/telemetry"
)

type LokiQueryProvider struct {
	client *loki.DefaultClient
}

func NewLokiQueryProvider(config cfg.Config) (*LokiQueryProvider, error) {
	lokiClient := &loki.DefaultClient{
		Address:  config.LokiURL,
		Username: config.LokiUser,
		Password: config.LokiPassword,
	}

	return &LokiQueryProvider{client: lokiClient}, nil
}

func (l *LokiQueryProvider) QueryBuildLogs(ctx context.Context, templateID string, buildID string, start time.Time, end time.Time, limit int, offset int32, level *logs.LogLevel, direction logproto.Direction) ([]logs.LogEntry, error) {
	// https://grafana.com/blog/2021/01/05/how-to-escape-special-characters-with-lokis-logql/
	templateIDSanitized := strings.ReplaceAll(templateID, "`", "")
	buildIDSanitized := strings.ReplaceAll(buildID, "`", "")

	// todo: service name is different here (because new merged orchestrator)
	query := fmt.Sprintf("{service=\"template-manager\", buildID=\"%s\", envID=`%s`}", buildIDSanitized, templateIDSanitized)

	res, err := l.client.QueryRange(query, limit, start, end, direction, time.Duration(0), time.Duration(0), true)
	if err != nil {
		telemetry.ReportError(ctx, "error when returning logs for template build", err)
		logger.L().Error(ctx, "error when returning logs for template build", zap.Error(err), logger.WithBuildID(buildID))

		return nil, fmt.Errorf("failed to query build logs: %w", err)
	}

	lm, err := logsloki.ResponseMapper(ctx, res, offset, level)
	if err != nil {
		telemetry.ReportError(ctx, "error when mapping build logs", err)
		logger.L().Error(ctx, "error when mapping logs for template build", zap.Error(err), logger.WithBuildID(buildID))

		return nil, fmt.Errorf("failed to map build logs: %w", err)
	}

	return lm, nil
}

func (l *LokiQueryProvider) QuerySandboxLogs(ctx context.Context, teamID string, sandboxID string, start time.Time, end time.Time, limit int, eventType string, direction logproto.Direction, includeSystemLogs bool) ([]logs.LogEntry, error) {
	// https://grafana.com/blog/2021/01/05/how-to-escape-special-characters-with-lokis-logql/
	sandboxIdSanitized := strings.ReplaceAll(sandboxID, "`", "")
	teamIdSanitized := strings.ReplaceAll(teamID, "`", "")

	var query string
	switch {
	case includeSystemLogs:
		// Admin view: include all logs (stdout, stderr, process_start, process_end, etc.)
		query = fmt.Sprintf("{teamID=`%s`, sandboxID=`%s`, category!=\"metrics\"}", teamIdSanitized, sandboxIdSanitized)
	case eventType == "stdout" || eventType == "stderr":
		// User view with specific event type filter - use label matching (fast)
		query = fmt.Sprintf("{teamID=`%s`, sandboxID=`%s`, category!=\"metrics\", event_type=\"%s\"}", teamIdSanitized, sandboxIdSanitized, eventType)
	default:
		// User view: filter to only stdout/stderr logs (user program output)
		// This excludes system logs like process_start, process_end
		query = fmt.Sprintf("{teamID=`%s`, sandboxID=`%s`, category!=\"metrics\", event_type=~\"stdout|stderr\"}", teamIdSanitized, sandboxIdSanitized)
	}

	res, err := l.client.QueryRange(query, limit, start, end, direction, time.Duration(0), time.Duration(0), true)
	if err != nil {
		telemetry.ReportError(ctx, "error when returning logs for sandbox", err)
		logger.L().Error(ctx, "error when returning logs for sandbox", zap.Error(err), logger.WithSandboxID(sandboxID))

		return nil, fmt.Errorf("failed to query sandbox logs: %w", err)
	}

	lm, err := logsloki.ResponseMapper(ctx, res, 0, nil)
	if err != nil {
		telemetry.ReportError(ctx, "error when mapping sandbox logs", err)
		logger.L().Error(ctx, "error when mapping logs for sandbox", zap.Error(err), logger.WithSandboxID(sandboxID))

		return nil, fmt.Errorf("failed to map sandbox logs: %w", err)
	}

	return lm, nil
}
