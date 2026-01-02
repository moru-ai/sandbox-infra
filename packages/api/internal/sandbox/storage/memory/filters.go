package memory

import (
	"github.com/moru-ai/sandbox-infra/packages/api/internal/sandbox"
)

// applyFilter checks if a sandbox matches the filter criteria
func applyFilter(sbx sandbox.Sandbox, filter *sandbox.ItemsFilter) bool {
	if filter.OnlyExpired && !sbx.IsExpired() {
		return false
	}

	return true
}
