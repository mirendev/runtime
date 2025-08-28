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
	ResourceUsage     ResourceUsage     `json:"resource_usage,omitempty"`
	HealthChecks      map[string]string `json:"health_checks,omitempty"`
	RBACRulesVersion  string            `json:"rbac_rules_version,omitempty"`
	LastRBACSync      *time.Time        `json:"last_rbac_sync,omitempty"`
	APIAddresses      []string          `json:"api_addresses,omitempty"`
	CACertFingerprint string            `json:"ca_cert_fingerprint,omitempty"`
}

// ReportClusterStatus sends a status report for the specified cluster
func (a *AuthClient) ReportClusterStatus(ctx context.Context, status *StatusReport) error {
	if status == nil {
		return fmt.Errorf("status cannot be nil")
	}

	if status.ClusterID == "" {
		return fmt.Errorf("cluster_id is required")
	}

	// Validate state field
	switch status.State {
	case "active", "degraded", "inactive", "unknown":
		// valid states
	case "":
		status.State = "unknown" // default to unknown if not specified
	default:
		return fmt.Errorf("invalid state: %s (must be one of: active, degraded, inactive, unknown)", status.State)
	}

	// Get authentication token
	token, err := a.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authentication token: %w", err)
	}

	// Prepare the request body
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status report: %w", err)
	}

	// Build the request URL
	url := fmt.Sprintf("%s/api/v1/clusters/%s/status", a.serverURL, status.ClusterID)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Send the request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send status report: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
			if errMsg, ok := errResp["error"].(string); ok {
				return fmt.Errorf("status report failed: %s", errMsg)
			}
		}
		return fmt.Errorf("status report failed with status code: %d", resp.StatusCode)
	}

	return nil
}
