package cloudauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportClusterStatus(t *testing.T) {
	// Create a test server to mock the cloud API
	var receivedStatus *StatusReport
	var authToken string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/service-account/begin":
			// Mock authentication begin
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(BeginAuthResponse{
				Envelope:  "test-envelope",
				Challenge: "dGVzdC1jaGFsbGVuZ2U=", // base64 encoded "test-challenge"
			})

		case "/auth/service-account/complete":
			// Mock authentication complete
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(CompleteAuthResponse{
				Token:     "test-jwt-token",
				ExpiresIn: 3600,
			})

		case "/api/v1/clusters/test-cluster-123/status":
			// Verify authorization header
			authToken = r.Header.Get("Authorization")

			// Parse and store the status report
			err := json.NewDecoder(r.Body).Decode(&receivedStatus)
			require.NoError(t, err)

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "accepted",
			})

		default:
			t.Fatalf("Unexpected request to %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	// Create a test key pair
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	// Create auth client
	authClient, err := NewAuthClient(ts.URL, keyPair)
	require.NoError(t, err)

	// Create status report
	now := time.Now()
	status := &StatusReport{
		ClusterID:     "test-cluster-123",
		Version:       "1.0.0",
		State:         "active",
		NodeCount:     3,
		WorkloadCount: 10,
		ResourceUsage: ResourceUsage{
			CPUCores:      4.5,
			CPUPercent:    56.25,
			MemoryBytes:   8589934592, // 8 GB
			MemoryPercent: 75.0,
		},
		HealthChecks: map[string]string{
			"etcd":       "healthy",
			"containerd": "healthy",
			"buildkit":   "healthy",
		},
		RBACRulesVersion:  "v1.2.3",
		LastRBACSync:      &now,
		APIAddresses:      []string{"cluster.example.com:8443", "10.0.0.1:8443"},
		CACertFingerprint: "1234567890abcdef1234567890abcdef12345678",
	}

	// Report status
	err = authClient.ReportClusterStatus(context.Background(), status)
	require.NoError(t, err)

	// Verify the status was received correctly
	assert.NotNil(t, receivedStatus)
	assert.Equal(t, "test-cluster-123", receivedStatus.ClusterID)
	assert.Equal(t, "1.0.0", receivedStatus.Version)
	assert.Equal(t, "active", receivedStatus.State)
	assert.Equal(t, 3, receivedStatus.NodeCount)
	assert.Equal(t, 10, receivedStatus.WorkloadCount)
	assert.Equal(t, 4.5, receivedStatus.ResourceUsage.CPUCores)
	assert.Equal(t, "healthy", receivedStatus.HealthChecks["etcd"])
	assert.Equal(t, "Bearer test-jwt-token", authToken)
}

func TestReportClusterStatus_Validation(t *testing.T) {
	// Create a test key pair
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	// Create auth client with dummy server
	authClient, err := NewAuthClient("http://localhost", keyPair)
	require.NoError(t, err)

	ctx := context.Background()

	// Test nil status
	err = authClient.ReportClusterStatus(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status cannot be nil")

	// Test empty cluster ID
	status := &StatusReport{
		State: "active",
	}
	err = authClient.ReportClusterStatus(ctx, status)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cluster_id is required")

	// Test invalid state
	status = &StatusReport{
		ClusterID: "test-cluster",
		State:     "invalid-state",
	}
	err = authClient.ReportClusterStatus(ctx, status)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid state")

	// Test default state (should not error)
	status = &StatusReport{
		ClusterID: "test-cluster",
		// State not set, should default to "unknown"
	}
	// This will fail to connect but won't fail validation
	_ = authClient.ReportClusterStatus(ctx, status)
	assert.Equal(t, "unknown", status.State)
}
