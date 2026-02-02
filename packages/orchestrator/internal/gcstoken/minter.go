// Package gcstoken provides functionality for minting downscoped GCS access tokens.
// Downscoped tokens allow sandboxes to access only their specific volume prefix in GCS.
// See: https://cloud.google.com/iam/docs/downscoping-short-lived-credentials
package gcstoken

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Token represents a downscoped GCS access token.
type Token struct {
	AccessToken string
	ExpiresIn   int       // seconds until expiry
	ExpiresAt   time.Time // absolute expiry time
}

// Minter creates downscoped GCS tokens for volume access.
type Minter struct {
	bucket     string
	httpClient *http.Client
}

// NewMinter creates a new token minter for the given GCS bucket.
func NewMinter(bucket string) *Minter {
	return &Minter{
		bucket: bucket,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// MintDownscopedToken creates a downscoped token that only allows access to the
// specified volume prefix within the bucket.
//
// The token is scoped to:
//   - Bucket: the configured volumes bucket
//   - Prefix: /{volumeID}/ (JuiceFS data chunks)
//   - Prefix: /{volumeID}-meta/ (Litestream metadata)
func (m *Minter) MintDownscopedToken(ctx context.Context, volumeID string) (*Token, error) {
	// Step 1: Get base token from GCP metadata server
	baseToken, err := m.getMetadataToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get metadata token: %w", err)
	}

	// Step 2: Create credential access boundary for this volume
	// We need to allow access to both the volume data and metadata prefixes
	cab := CredentialAccessBoundary{
		AccessBoundary: AccessBoundary{
			AccessBoundaryRules: []AccessBoundaryRule{
				{
					// Volume data: /{volumeID}/
					AvailablePermissions: []string{"inRole:roles/storage.objectAdmin"},
					AvailableResource:    fmt.Sprintf("//storage.googleapis.com/projects/_/buckets/%s", m.bucket),
					AvailabilityCondition: &AvailabilityCondition{
						Expression: fmt.Sprintf(
							`resource.name.startsWith("projects/_/buckets/%s/objects/%s/")`,
							m.bucket, volumeID,
						),
					},
				},
				{
					// Volume metadata (Litestream): /{volumeID}-meta/
					AvailablePermissions: []string{"inRole:roles/storage.objectAdmin"},
					AvailableResource:    fmt.Sprintf("//storage.googleapis.com/projects/_/buckets/%s", m.bucket),
					AvailabilityCondition: &AvailabilityCondition{
						Expression: fmt.Sprintf(
							`resource.name.startsWith("projects/_/buckets/%s/objects/%s-meta/")`,
							m.bucket, volumeID,
						),
					},
				},
			},
		},
	}

	// Step 3: Exchange for downscoped token via STS
	return m.exchangeToken(ctx, baseToken, cab)
}

// getMetadataToken retrieves an access token from the GCP metadata server.
func (m *Minter) getMetadataToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("metadata server returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return tokenResp.AccessToken, nil
}

// exchangeToken exchanges a base token for a downscoped token via GCP STS.
func (m *Minter) exchangeToken(ctx context.Context, baseToken string, cab CredentialAccessBoundary) (*Token, error) {
	cabJSON, err := json.Marshal(cab)
	if err != nil {
		return nil, fmt.Errorf("marshal CAB: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	data.Set("subject_token", baseToken)
	data.Set("options", string(cabJSON))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://sts.googleapis.com/v1/token",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request STS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("STS returned %d: %s", resp.StatusCode, string(body))
	}

	var stsResp struct {
		AccessToken     string `json:"access_token"`
		IssuedTokenType string `json:"issued_token_type"`
		TokenType       string `json:"token_type"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stsResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &Token{
		AccessToken: stsResp.AccessToken,
		ExpiresIn:   stsResp.ExpiresIn,
		ExpiresAt:   time.Now().Add(time.Duration(stsResp.ExpiresIn) * time.Second),
	}, nil
}

// CredentialAccessBoundary defines the scope restrictions for a downscoped token.
type CredentialAccessBoundary struct {
	AccessBoundary AccessBoundary `json:"accessBoundary"`
}

// AccessBoundary contains the rules for token scoping.
type AccessBoundary struct {
	AccessBoundaryRules []AccessBoundaryRule `json:"accessBoundaryRules"`
}

// AccessBoundaryRule defines a single access rule for a resource.
type AccessBoundaryRule struct {
	AvailablePermissions  []string               `json:"availablePermissions"`
	AvailableResource     string                 `json:"availableResource"`
	AvailabilityCondition *AvailabilityCondition `json:"availabilityCondition,omitempty"`
}

// AvailabilityCondition is a CEL expression that further restricts access.
type AvailabilityCondition struct {
	Expression string `json:"expression"`
}
