package sandbox

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"miren.dev/runtime/pkg/workloadidentity"
)

type tokenResponse struct {
	Value string `json:"value"`
}

func (c *SandboxController) handleTokenRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMetadataError(w, http.StatusMethodNotAllowed, "only GET is allowed")
		return
	}

	sandboxID, appName, ok := c.authenticateWorkload(w, r)
	if !ok {
		return
	}

	opts := workloadidentity.TokenOptions{}
	if auds := r.URL.Query()["audience"]; len(auds) > 0 {
		opts.Audience = auds
	}
	if ttlStr := r.URL.Query().Get("ttl"); ttlStr != "" {
		ttlSec, err := strconv.Atoi(ttlStr)
		if err != nil || ttlSec <= 0 {
			writeMetadataError(w, http.StatusBadRequest, "ttl must be a positive integer (seconds)")
			return
		}
		opts.TTL = time.Duration(ttlSec) * time.Second
	}

	token, err := c.WorkloadIssuer.IssueTokenWithOptions(appName, sandboxID, opts)
	if err != nil {
		c.Log.Error("failed to issue token", "sandbox", sandboxID, "error", err)
		writeMetadataError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResponse{Value: token})
}
