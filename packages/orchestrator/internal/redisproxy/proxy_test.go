package redisproxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUpstreamURL(t *testing.T) {
	tests := []struct {
		name           string
		rawURL         string
		wantHost       string
		wantTLS        bool
		wantSkipVerify bool
		wantErr        bool
	}{
		{
			name:     "plain redis with port",
			rawURL:   "redis://10.0.0.1:6379",
			wantHost: "10.0.0.1:6379",
			wantTLS:  false,
		},
		{
			name:     "plain redis without port",
			rawURL:   "redis://10.0.0.1",
			wantHost: "10.0.0.1:6379",
			wantTLS:  false,
		},
		{
			name:     "rediss with TLS",
			rawURL:   "rediss://10.0.0.1:6379",
			wantHost: "10.0.0.1:6379",
			wantTLS:  true,
		},
		{
			name:           "rediss with insecure-skip-verify",
			rawURL:         "rediss://10.0.0.1:6379?insecure-skip-verify=true",
			wantHost:       "10.0.0.1:6379",
			wantTLS:        true,
			wantSkipVerify: true,
		},
		{
			name:     "rediss without insecure-skip-verify",
			rawURL:   "rediss://10.0.0.1:6379?insecure-skip-verify=false",
			wantHost: "10.0.0.1:6379",
			wantTLS:  true,
		},
		{
			name:     "hostname instead of IP",
			rawURL:   "redis://redis.example.com:6380",
			wantHost: "redis.example.com:6380",
			wantTLS:  false,
		},
		{
			name:    "unsupported scheme",
			rawURL:  "http://10.0.0.1:6379",
			wantErr: true,
		},
		{
			name:    "empty host",
			rawURL:  "redis:///database",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			rawURL:  "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseUpstreamURL(tt.rawURL)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, cfg.host)

			if tt.wantTLS {
				require.NotNil(t, cfg.tlsConfig)
				assert.Equal(t, tt.wantSkipVerify, cfg.tlsConfig.InsecureSkipVerify)
			} else {
				assert.Nil(t, cfg.tlsConfig)
			}
		})
	}
}
