package edge

import (
	"context"

	"go.opentelemetry.io/otel"

	"github.com/moru-ai/sandbox-infra/packages/proxy/internal/cfg"
	"github.com/moru-ai/sandbox-infra/packages/proxy/internal/edge/handlers"
	moruinfo "github.com/moru-ai/sandbox-infra/packages/proxy/internal/edge/info"
	moruorchestrators "github.com/moru-ai/sandbox-infra/packages/proxy/internal/edge/pool"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
	catalog "github.com/moru-ai/sandbox-infra/packages/shared/pkg/sandbox-catalog"
)

var tracer = otel.Tracer("github.com/moru-ai/sandbox-infra/packages/client-proxy/internal/edge")

func NewEdgeAPIStore(
	ctx context.Context,
	l logger.Logger,
	info *moruinfo.ServiceInfo,
	orchestrators *moruorchestrators.OrchestratorsPool,
	catalog catalog.SandboxesCatalog,
	config cfg.Config,
) (*handlers.APIStore, error) {
	store, err := handlers.NewStore(ctx, l, info, orchestrators, catalog, config)
	if err != nil {
		return nil, err
	}

	return store, nil
}
