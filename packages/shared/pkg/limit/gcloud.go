package limit

import (
	"context"

	featureflags "github.com/moru-ai/sandbox-infra/packages/shared/pkg/feature-flags"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/utils"
)

func (l *Limiter) GCloudUploadLimiter() *utils.AdjustableSemaphore {
	return l.gCloudUploadLimiter
}

func (l *Limiter) GCloudMaxTasks(ctx context.Context) int {
	maxTasks := l.featureFlags.IntFlag(ctx, featureflags.GcloudMaxTasks)

	return maxTasks
}
