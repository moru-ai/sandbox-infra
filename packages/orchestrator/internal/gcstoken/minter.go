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
	bucket                    string
	impersonateServiceAccount string // SA email to impersonate (optional)
	httpClient                *http.Client
}

// NewMinter creates a new token minter for the given GCS bucket.
// If impersonateSA is provided, the minter will impersonate that service account
// when generating tokens (recommended for security isolation).
func NewMinter(bucket string, impersonateSA string) *Minter {
	return &Minter{
		bucket:                    bucket,
		impersonateServiceAccount: impersonateSA,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// MintDownscopedToken creates a downscoped token for volume operations.
// The token is scoped to the specific volume prefix with minimal permissions:
//   - objectAdmin: list + get + create (restricted to volumeID/ and volumeID-meta/ prefixes)
//
// Uses CAB availabilityCondition with:
//   - resource.name.startsWith() for GET/PUT operations
//   - api.getAttribute('storage.googleapis.com/objectListPrefix') for LIST operations
func (m *Minter) MintDownscopedToken(ctx context.Context, volumeID string) (*Token, error) {
	// Step 1: Get base token (either via impersonation or directly from metadata)
	baseToken, err := m.getBaseToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get base token: %w", err)
	}

	// Step 2: Create credential access boundary with minimal permissions
	// Using objectAdmin with CEL condition to restrict to volume prefix
	bucketResource := fmt.Sprintf("//storage.googleapis.com/projects/_/buckets/%s", m.bucket)

	// Build CEL condition for volume isolation
	// - resource.name.startsWith() for GET/PUT operations
	// - api.getAttribute('storage.googleapis.com/objectListPrefix') for LIST operations
	prefixCondition := fmt.Sprintf(
		"resource.name.startsWith('projects/_/buckets/%s/objects/%s/') || "+
			"resource.name.startsWith('projects/_/buckets/%s/objects/%s-meta/') || "+
			"api.getAttribute('storage.googleapis.com/objectListPrefix', '').startsWith('%s/') || "+
			"api.getAttribute('storage.googleapis.com/objectListPrefix', '').startsWith('%s-meta/')",
		m.bucket, volumeID, m.bucket, volumeID, volumeID, volumeID,
	)

	cab := CredentialAccessBoundary{
		AccessBoundary: AccessBoundary{
			AccessBoundaryRules: []AccessBoundaryRule{
				{
					AvailablePermissions: []string{"inRole:roles/storage.objectAdmin"},
					AvailableResource:    bucketResource,
					AvailabilityCondition: &AvailabilityCondition{
						Title:      "Volume isolation",
						Expression: prefixCondition,
					},
				},
			},
		},
	}

	// Step 3: Exchange for downscoped token via STS
	return m.exchangeToken(ctx, baseToken, cab)
}

// getBaseToken gets the token to be downscoped.
// If impersonation is configured, it generates an access token for the target SA.
// Otherwise, it uses the VM's default service account token.
func (m *Minter) getBaseToken(ctx context.Context) (string, error) {
	if m.impersonateServiceAccount != "" {
		return m.getImpersonatedToken(ctx)
	}
	return m.getMetadataToken(ctx)
}

// getImpersonatedToken generates an access token by impersonating another service account.
// This requires the caller to have the iam.serviceAccountTokenCreator role on the target SA.
func (m *Minter) getImpersonatedToken(ctx context.Context) (string, error) {
	// First get our own token to authenticate the impersonation request
	callerToken, err := m.getMetadataToken(ctx)
	if err != nil {
		return "", fmt.Errorf("get caller token: %w", err)
	}

	// Generate access token for the target service account
	// https://cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken
	url := fmt.Sprintf(
		"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/%s:generateAccessToken",
		m.impersonateServiceAccount,
	)

	body := map[string]interface{}{
		"scope": []string{
			"https://www.googleapis.com/auth/devstorage.full_control",
		},
		"lifetime": "3600s", // 1 hour max
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+callerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request impersonation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("impersonation failed with %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"accessToken"`
		ExpireTime  string `json:"expireTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return tokenResp.AccessToken, nil
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
	Title      string `json:"title,omitempty"`
	Expression string `json:"expression"`
}
