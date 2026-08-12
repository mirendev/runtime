package cloudauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ResourceUsage represents resource utilization metrics
type ResourceUsage struct {
	CPUCores       float64 `json:"cpu_cores,omitempty"`
	CPUPercent     float64 `json:"cpu_percent,omitempty"`
	MemoryBytes    int64   `json:"memory_bytes,omitempty"`
	MemoryPercent  float64 `json:"memory_percent,omitempty"`
	StorageBytes   int64   `json:"storage_bytes,omitempty"`
	StoragePercent float64 `json:"storage_percent,omitempty"`
}

// StatusReport represents the cluster status to report
type StatusReport struct {
	ClusterID         string            `json:"cluster_id"`
	Version           string            `json:"version,omitempty"`
	State             string            `json:"state"` // required: active, degraded, inactive, unknown
	NodeCount         int               `json:"node_count,omitempty"`
	WorkloadCount     int               `json:"workload_count,omitempty"`
	ResourceUsage     ResourceUsage     `json:"resource_usage"`
	HealthChecks      map[string]string `json:"health_checks,omitempty"`
	RBACRulesVersion  string            `json:"rbac_rules_version,omitempty"`
	LastRBACSync      *time.Time        `json:"last_rbac_sync,omitempty"`
	APIAddresses      []string          `json:"api_addresses,omitempty"`
	CACertFingerprint string            `json:"ca_cert_fingerprint,omitempty"`
	// Reachability, when non-nil, carries the agent's verdict on whether the
	// cluster's public address is reachable from the internet and, if not,
	// which ports failed. Omitted (nil) when netcheck never produced a usable
	// public source address, so old agents and pre-netcheck reports simply
	// don't send it and cloud falls back to its generic copy.
	Reachability *ReachabilityVerdict `json:"reachability,omitempty"`

	// Containerized reports whether the miren server is running inside a
	// container (Docker, Podman, or a Kubernetes pod). A containerized server is
	// effectively never reachable directly from the internet, so miren.cloud
	// uses this to keep Miren Anywhere (POP routing) forced on for the cluster.
	// Sent unconditionally (no omitempty) so an explicit false keeps cloud in
	// sync if a cluster ever moves off a container.
	Containerized bool `json:"containerized"`
}

// ReachabilityVerdict is a compact, agent-computed explanation of the
// cluster's inbound reachability, synthesized from the netcheck trace at
// report time. It exists so the dashboard can name the culprit ("found
// 159.195.16.123 but UDP 8443 (QUIC) unreachable") instead of showing a
// generic "Not reachable" with static guess-at-the-fix copy.
type ReachabilityVerdict struct {
	// Reachable is true when netcheck confirmed at least one reachable port
	// on a public source address. Deliberately no omitempty: a false value is
	// the interesting case and must travel on the wire.
	Reachable bool `json:"reachable"`
	// PublicAddress is the public source address netcheck observed (the IP the
	// cluster appears as from the internet), e.g. "159.195.16.123".
	PublicAddress string `json:"public_address,omitempty"`
	// UnreachablePorts lists the ports that failed the check, present only
	// when Reachable is false.
	UnreachablePorts []UnreachablePort `json:"unreachable_ports,omitempty"`
}

// UnreachablePort names a single port/protocol that netcheck could not reach,
// with its transport spelled out so the dashboard can say "UDP 8443 (QUIC)".
type UnreachablePort struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`  // "http3", "https", "http"
	Transport string `json:"transport"` // "UDP" (QUIC/http3) or "TCP"
}

// ReportClusterStatus sends a status report for the specified cluster
// StatusResponse is what cloud answers a status report with.
type StatusResponse struct {
	Message string `json:"message"`
	// IdentityIssuerURL is where cloud would anchor this cluster's workload
	// identity, repeated on every report so a cluster registered before anchors
	// existed can learn its own without re-registering. Empty when cloud is not
	// serving discovery.
	IdentityIssuerURL string `json:"identity_issuer_url,omitempty"`
}

func (a *AuthClient) ReportClusterStatus(ctx context.Context, status *StatusReport) (*StatusResponse, error) {
	if status == nil {
		return nil, fmt.Errorf("status cannot be nil")
	}

	if status.ClusterID == "" {
		return nil, fmt.Errorf("cluster_id is required")
	}

	// Validate state field
	switch status.State {
	case "active", "degraded", "inactive", "unknown":
		// valid states
	case "":
		status.State = "unknown" // default to unknown if not specified
	default:
		return nil, fmt.Errorf("invalid state: %s (must be one of: active, degraded, inactive, unknown)", status.State)
	}

	// Get authentication token
	token, err := a.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get authentication token: %w", err)
	}

	// Prepare the request body
	body, err := json.Marshal(status)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal status report: %w", err)
	}

	// Build the request URL
	url := fmt.Sprintf("%s/api/v1/clusters/%s/status", a.serverURL, status.ClusterID)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Send the request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send status report: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			if errMsg, ok := errResp["error"].(string); ok {
				return nil, fmt.Errorf("status report failed: %s", errMsg)
			}
		}
		return nil, fmt.Errorf("status report failed with status code: %d", resp.StatusCode)
	}

	// The report itself succeeded, so a body we cannot read costs the anchor
	// and nothing else. Reporting it as a failed status report would be a lie
	// that also stops the retry loop from making progress.
	var result StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &StatusResponse{}, nil
	}

	return &result, nil
}
