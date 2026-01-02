package template_manager

import (
	"github.com/moru-ai/sandbox-infra/packages/api/internal/edge"
)

type BuildClient struct {
	GRPC *edge.ClusterGRPC
}
