package filesystem

import (
	"github.com/moru-ai/sandbox-infra/packages/envd/internal/execcontext"
	"github.com/moru-ai/sandbox-infra/packages/envd/internal/utils"
)

func mockService() Service {
	return Service{
		defaults: &execcontext.Defaults{
			EnvVars: utils.NewMap[string, string](),
		},
	}
}
