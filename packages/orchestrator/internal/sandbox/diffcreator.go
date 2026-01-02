package sandbox

import (
	"context"
	"io"

	"github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/sandbox/rootfs"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/storage/header"
)

type DiffCreator interface {
	process(ctx context.Context, out io.Writer) (*header.DiffMetadata, error)
}

type RootfsDiffCreator struct {
	rootfs    rootfs.Provider
	closeHook func(context.Context) error
}

func (r *RootfsDiffCreator) process(ctx context.Context, out io.Writer) (*header.DiffMetadata, error) {
	return r.rootfs.ExportDiff(ctx, out, r.closeHook)
}
