package cloudauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrClusterNotRegistered reports that miren.cloud has no cluster for this
// service account. Unregistering is meant to be safe to retry, so callers
// treat this as "the cloud side is already done" rather than a failure.
var ErrClusterNotRegistered = errors.New("cluster not registered with cloud")

// UnregisterSelf asks miren.cloud to remove the cluster this client
// authenticates as, revoking its service account along the way. It is the
// cloud half of `miren server unregister` — the cluster identifies itself with
// the service account key from its registration, so no user login is involved.
func (a *AuthClient) UnregisterSelf(ctx context.Context) error {
	token, err := a.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authentication token: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/self/cluster", a.serverURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send unregister request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrClusterNotRegistered
	}

	var errResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
		if errMsg, ok := errResp["error"].(string); ok {
			return fmt.Errorf("unregister failed: %s", errMsg)
		}
	}

	return fmt.Errorf("unregister failed with status code: %d", resp.StatusCode)
}
