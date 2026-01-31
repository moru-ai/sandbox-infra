package fc

// The metadata serialization should not be changed — it is different from the field names we use here!
type MmdsMetadata struct {
	SandboxID  string `json:"instanceID"`
	TemplateID string `json:"envID"`

	LogsCollectorAddress string `json:"address"`

	// Volume configuration for persistent storage (optional).
	Volume *MmdsVolumeConfig `json:"volume,omitempty"`
}

// MmdsVolumeConfig contains volume configuration for envd to mount.
type MmdsVolumeConfig struct {
	// VolumeID is the volume identifier (e.g., "vol_abc123").
	VolumeID string `json:"volumeId"`

	// MountPath is the path where the volume should be mounted (e.g., "/workspace").
	MountPath string `json:"mountPath"`

	// RedisDB is the database number for JuiceFS metadata key prefix.
	RedisDB int `json:"redisDb"`

	// GCSBucket is the bucket name for volume data storage.
	GCSBucket string `json:"gcsBucket"`

	// ProxyHost is the host address for GCS and Redis proxies (e.g., "10.12.0.1").
	ProxyHost string `json:"proxyHost"`
}
