package commands

import (
	"context"

	"go.uber.org/zap/zapcore"

	"github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/proxy"
	"github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/template/metadata"
	templatemanager "github.com/moru-ai/sandbox-infra/packages/shared/pkg/grpc/template-manager"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

type Command interface {
	Execute(
		ctx context.Context,
		logger logger.Logger,
		lvl zapcore.Level,
		proxy *proxy.SandboxProxy,
		sandboxID string,
		prefix string,
		step *templatemanager.TemplateStep,
		cmdMetadata metadata.Context,
	) (metadata.Context, error)
}
