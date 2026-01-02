package handlers

import (
	"context"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	moruorchestrators "github.com/moru-ai/sandbox-infra/packages/proxy/internal/edge/pool"
	morugrpcorchestratorinfo "github.com/moru-ai/sandbox-infra/packages/shared/pkg/grpc/orchestrator-info"
	api "github.com/moru-ai/sandbox-infra/packages/shared/pkg/http/edge"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/logger"
)

func (a *APIStore) V1ServiceDiscoveryGetOrchestrators(c *gin.Context) {
	_, templateSpan := tracer.Start(c, "service-discovery-list-orchestrators-handler")
	defer templateSpan.End()

	ctx := c.Request.Context()

	response := make([]api.ClusterOrchestratorNode, 0)

	for _, node := range a.orchestratorPool.GetOrchestrators() {
		info := node.GetInfo()
		response = append(
			response,
			api.ClusterOrchestratorNode{
				NodeID:            info.NodeID,
				ServiceInstanceID: info.ServiceInstanceID,

				ServiceVersion:       info.ServiceVersion,
				ServiceVersionCommit: info.ServiceVersionCommit,
				ServiceHost:          info.Host,
				ServiceStartedAt:     info.ServiceStartup,
				ServiceStatus:        getOrchestratorStatusResolved(ctx, info.ServiceStatus),

				Roles: getOrchestratorRolesResolved(ctx, info.Roles),
			},
		)
	}

	sort.Slice(
		response,
		func(i, j int) bool {
			// older dates first
			return response[i].ServiceStartedAt.Before(response[j].ServiceStartedAt)
		},
	)

	c.JSON(http.StatusOK, response)
}

func getOrchestratorStatusResolved(ctx context.Context, s moruorchestrators.OrchestratorStatus) api.ClusterNodeStatus {
	switch s {
	case moruorchestrators.OrchestratorStatusHealthy:
		return api.Healthy
	case moruorchestrators.OrchestratorStatusDraining:
		return api.Draining
	case moruorchestrators.OrchestratorStatusUnhealthy:
		return api.Unhealthy
	default:
		logger.L().Error(ctx, "Unknown orchestrator status", zap.String("status", string(s)))

		return api.Unhealthy
	}
}

func getOrchestratorRolesResolved(ctx context.Context, r []morugrpcorchestratorinfo.ServiceInfoRole) []api.ClusterOrchestratorRole {
	roles := make([]api.ClusterOrchestratorRole, 0)

	for _, role := range r {
		switch role {
		case morugrpcorchestratorinfo.ServiceInfoRole_Orchestrator:
			roles = append(roles, api.ClusterOrchestratorRoleOrchestrator)
		case morugrpcorchestratorinfo.ServiceInfoRole_TemplateBuilder:
			roles = append(roles, api.ClusterOrchestratorRoleTemplateBuilder)
		default:
			logger.L().Error(ctx, "Unknown orchestrator role", zap.String("role", string(role)))
		}
	}

	return roles
}
