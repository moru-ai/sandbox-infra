package cfg

import "github.com/caarlos0/env/v11"

const (
	DefaultKernelVersion = "vmlinux-6.1.158"
)

type Config struct {
	AdminToken string `env:"ADMIN_TOKEN"`

	AnalyticsCollectorAPIToken string `env:"ANALYTICS_COLLECTOR_API_TOKEN"`
	AnalyticsCollectorHost     string `env:"ANALYTICS_COLLECTOR_HOST"`

	ClickhouseConnectionString string `env:"CLICKHOUSE_CONNECTION_STRING"`

	LocalClusterEndpoint string `env:"LOCAL_CLUSTER_ENDPOINT"`
	LocalClusterToken    string `env:"LOCAL_CLUSTER_TOKEN"`

	NomadAddress string `env:"NOMAD_ADDRESS" envDefault:"http://localhost:4646"`
	NomadToken   string `env:"NOMAD_TOKEN"`

	PostgresConnectionString string `env:"POSTGRES_CONNECTION_STRING,required,notEmpty"`

	PosthogAPIKey string `env:"POSTHOG_API_KEY"`

	RedisURL         string `env:"REDIS_URL"`
	RedisClusterURL  string `env:"REDIS_CLUSTER_URL"`
	RedisTLSCABase64 string `env:"REDIS_TLS_CA_BASE64"`

	SandboxAccessTokenHashSeed string `env:"SANDBOX_ACCESS_TOKEN_HASH_SEED"`

	// SupabaseJWTSecrets is a list of secrets used to verify the Supabase JWT.
	// More secrets are possible in the case of JWT secret rotation where we need to accept
	// tokens signed with the old secret for some time.
	SupabaseJWTSecrets []string `env:"SUPABASE_JWT_SECRETS"`

	DefaultKernelVersion string `env:"DEFAULT_KERNEL_VERSION"`

	// VolumesBucket is the GCS bucket for volume data storage.
	VolumesBucket string `env:"VOLUMES_BUCKET"`

	// VolumesRedisURL is the Redis URL for JuiceFS volume metadata.
	VolumesRedisURL string `env:"VOLUMES_REDIS_URL"`

	// VolumesEncryptionKey is the base64-encoded 256-bit key for encrypting volume passwords.
	// Generate with: openssl rand -base64 32
	VolumesEncryptionKey string `env:"VOLUMES_ENCRYPTION_KEY"`
}

func Parse() (Config, error) {
	var config Config
	err := env.Parse(&config)

	if config.DefaultKernelVersion == "" {
		config.DefaultKernelVersion = DefaultKernelVersion
	}

	return config, err
}
