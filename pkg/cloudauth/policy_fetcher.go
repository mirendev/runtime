package cloudauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"miren.dev/runtime/pkg/rbac"
)

// PolicyFetcher is a one-shot HTTP policy reader for the offline debug CLI.
// Running clusters authorize from AuthorizationState, never this endpoint.
type PolicyFetcher struct {
	cloudURL   string
	authClient *AuthClient
	policy     *rbac.Policy
}

func NewPolicyFetcher(cloudURL string, authClient *AuthClient) *PolicyFetcher {
	return &PolicyFetcher{cloudURL: cloudURL, authClient: authClient}
}

func (pf *PolicyFetcher) Fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pf.cloudURL+"/api/v1/self/rbac-rules", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if pf.authClient != nil {
		token, err := pf.authClient.GetToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get auth token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "miren-runtime/1.0")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch policy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch policy: status %d", resp.StatusCode)
	}
	var policy rbac.Policy
	if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
		return fmt.Errorf("failed to parse policy: %w", err)
	}
	pf.policy = &policy
	return nil
}

func (pf *PolicyFetcher) GetPolicy() *rbac.Policy { return pf.policy }
