package utils

import (
	"github.com/google/uuid"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/consts"
)

func WithClusterFallback(clusterID *uuid.UUID) uuid.UUID {
	if clusterID == nil {
		return consts.LocalClusterID
	}

	return *clusterID
}
