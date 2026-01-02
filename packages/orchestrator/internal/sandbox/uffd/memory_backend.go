package uffd

import (
	"context"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/storage/header"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/utils"
)

type MemoryBackend interface {
	DiffMetadata(ctx context.Context) (*header.DiffMetadata, error)
	Start(ctx context.Context, sandboxId string) error
	Stop() error
	Ready() chan struct{}
	Exit() *utils.ErrorOnce
}
