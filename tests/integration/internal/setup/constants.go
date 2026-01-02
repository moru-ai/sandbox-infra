package setup

import (
	"os"
	"time"

	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/utils"
)

const (
	apiTimeout  = 120 * time.Second
	envdTimeout = 600 * time.Second
)

var (
	APIServerURL      = utils.RequiredEnv("TESTS_API_SERVER_URL", "e.g. https://api.great-innovations.dev")
	SandboxTemplateID = utils.RequiredEnv("TESTS_SANDBOX_TEMPLATE_ID", "e.g. base")
	APIKey            = utils.RequiredEnv("TESTS_MORU_API_KEY", "your Team API key")
	AccessToken       = utils.RequiredEnv("TESTS_MORU_ACCESS_TOKEN", "your Access token")

	SupabaseJWTSecret = os.Getenv("TESTS_SUPABASE_JWT_SECRET")

	TeamID = os.Getenv("TESTS_SANDBOX_TEAM_ID")
	UserID = os.Getenv("TESTS_SANDBOX_USER_ID")

	OrchestratorHost = os.Getenv("TESTS_ORCHESTRATOR_HOST")
	EnvdProxy        = os.Getenv("TESTS_ENVD_PROXY")
)
