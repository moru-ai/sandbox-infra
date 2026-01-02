package healthcheck

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/service"
	moruorchestratorinfo "github.com/moru-ai/sandbox-infra/packages/shared/pkg/grpc/orchestrator-info"
	moruHealth "github.com/moru-ai/sandbox-infra/packages/shared/pkg/health"
)

type Healthcheck struct {
	info *service.ServiceInfo

	lastRun time.Time
	mu      sync.RWMutex
}

func NewHealthcheck(info *service.ServiceInfo) (*Healthcheck, error) {
	return &Healthcheck{
		info: info,

		lastRun: time.Now(),
		mu:      sync.RWMutex{},
	}, nil
}

func (h *Healthcheck) CreateHandler() http.Handler {
	// Start /health HTTP server
	routeMux := http.NewServeMux()
	routeMux.HandleFunc("/health", h.healthHandler)

	return routeMux
}

func (h *Healthcheck) getStatus() moruHealth.Status {
	switch h.info.GetStatus() {
	case moruorchestratorinfo.ServiceInfoStatus_Healthy:
		return moruHealth.Healthy
	case moruorchestratorinfo.ServiceInfoStatus_Draining:
		return moruHealth.Draining
	}

	return moruHealth.Unhealthy
}

func (h *Healthcheck) healthHandler(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := h.getStatus()
	response := moruHealth.Response{Status: status, Version: h.info.SourceCommit}

	w.Header().Set("Content-Type", "application/json")
	if status == moruHealth.Unhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
