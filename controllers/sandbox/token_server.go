package sandbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/metricspush"
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

// metricsPushEnabled reports whether this node can authenticate and relay pushes.
func (c *SandboxController) metricsPushEnabled() bool {
	return c.MetricsPusher != nil && c.WorkloadIssuer != nil && c.tokenSecrets != nil
}

func (c *SandboxController) relayAuthenticator(remoteHost, secret string) (string, string, bool) {
	sandboxID, appName, err := c.authenticateSandbox(remoteHost, secret)
	return sandboxID, appName, err == nil
}

// otlpMetricsEnv leaves an application's own OTLP configuration untouched.
func otlpMetricsEnv(env []string, relayBase, secret string) []string {
	for _, kv := range env {
		if strings.HasPrefix(kv, "OTEL_EXPORTER_OTLP_") {
			return nil
		}
	}
	return []string{
		fmt.Sprintf("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=%s/%s/otlp/v1/metrics", relayBase, metricspush.ScopeSandbox),
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS=Authorization=Bearer%20" + secret,
	}
}
