package metrics_provider

import (
	clickhouse "github.com/moru-ai/sandbox-infra/packages/clickhouse/pkg"
	"github.com/moru-ai/sandbox-infra/packages/proxy/internal/cfg"
)

func GetSandboxMetricsQueryProvider(config cfg.Config) (clickhouse.SandboxQueriesProvider, error) {
	if config.ClickhouseConnectionString == "" {
		return clickhouse.NewNoopClient(), nil
	}

	return clickhouse.New(config.ClickhouseConnectionString)
}
