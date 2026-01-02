package user

import (
	"context"

	"github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/template/build/phases"
	"github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/template/build/storage/cache"
)

const prefix = "DEFAULT USER"

func (ub *UserBuilder) Hash(_ context.Context, sourceLayer phases.LayerResult) (string, error) {
	return cache.HashKeys(
		sourceLayer.Hash,
		prefix,
		ub.user,
	), nil
}
