package cloudauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrDiscoveryUnavailable reports that miren.cloud is not configured to anchor
// workload identity. Nothing the cluster can do about it, so callers keep their
// existing anchor rather than retrying into a wall.
var ErrDiscoveryUnavailable = errors.New("cloud workload identity discovery is unavailable")

// PublishJWKSResult is where miren.cloud has anchored this cluster's workload
// identity — the iss claim its tokens should carry and the URL verifiers will
// fetch keys from.
type PublishJWKSResult struct {
	Issuer   string `json:"issuer"`
	JWKSURI  string `json:"jwks_uri"`
	KeyCount int    `json:"key_count"`
}

// PublishJWKS sends the public half of this cluster's workload identity signing
// keys to miren.cloud, which serves them as OIDC discovery on the cluster's
// behalf.
//
// The private keys never leave this machine and cloud never sees them: it holds
// the anchor and the public material, so a compromise there cannot mint an
// identity. What cloud provides in exchange is a stable, publicly reachable
// discovery endpoint, which is what lets a cluster behind NAT federate to AWS
// or GCP at all and keeps federation working while the cluster is down.
//
// The document is the cluster's complete current key set. Cloud treats it as
// authoritative and stops serving anything absent from it, which is the whole
// of the rotation protocol: rotate locally, publish, done.
func (a *AuthClient) PublishJWKS(ctx context.Context, jwksDocument []byte) (*PublishJWKSResult, error) {
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get authentication token: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/self/cluster/jwks", a.serverURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(jwksDocument))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/jwk-set+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to publish signing keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result PublishJWKSResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode publish response: %w", err)
		}
		if result.Issuer == "" {
			return nil, fmt.Errorf("cloud accepted the key set but assigned no issuer")
		}
		return &result, nil
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, ErrDiscoveryUnavailable
	}

	// Cloud names the specific reason a key set was rejected, and that reason is
	// the difference between a quick fix and an opaque failure to federate.
	var errResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
		if errMsg, ok := errResp["error"].(string); ok {
			return nil, fmt.Errorf("publishing signing keys failed: %s", errMsg)
		}
	}

	return nil, fmt.Errorf("publishing signing keys failed with status code: %d", resp.StatusCode)
}
