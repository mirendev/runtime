package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/registration"
)

func anchorTestContext(t *testing.T) *Context {
	t.Helper()
	return &Context{Context: context.Background(), Stdout: &bytes.Buffer{}}
}

func writeRegistration(t *testing.T, dataPath string, reg *registration.StoredRegistration) string {
	t.Helper()

	dir := filepath.Join(dataPath, "server")
	require.NoError(t, registration.SaveRegistration(dir, reg))
	return dir
}

func approvedRegistration() *registration.StoredRegistration {
	return &registration.StoredRegistration{
		ClusterID:         "cluster-abc",
		ClusterName:       "test",
		OrganizationID:    "org-1",
		Status:            "approved",
		DNSHostname:       "cluster-abc.miren.systems",
		IdentityIssuerURL: "https://api.miren.cloud/identity/cluster-abc",
		PrivateKey:        "unused",
	}
}

func loadRegistration(t *testing.T, dir string) *registration.StoredRegistration {
	t.Helper()

	reg, err := registration.LoadRegistration(dir)
	require.NoError(t, err)
	require.NotNil(t, reg)
	return reg
}

func TestIdentityAnchorMovesToCloud(t *testing.T) {
	dataPath := t.TempDir()
	dir := writeRegistration(t, dataPath, approvedRegistration())

	require.NoError(t, IdentityAnchor(anchorTestContext(t), IdentityAnchorOptions{
		Anchor:   registration.AnchorCloud,
		DataPath: dataPath,
	}))

	require.Equal(t, registration.AnchorCloud, loadRegistration(t, dir).IdentityAnchor)
}

func TestIdentityAnchorMovesBackToCluster(t *testing.T) {
	dataPath := t.TempDir()
	reg := approvedRegistration()
	reg.IdentityAnchor = registration.AnchorCloud
	dir := writeRegistration(t, dataPath, reg)

	require.NoError(t, IdentityAnchor(anchorTestContext(t), IdentityAnchorOptions{
		Anchor:   registration.AnchorCluster,
		DataPath: dataPath,
	}))

	require.Equal(t, registration.AnchorCluster, loadRegistration(t, dir).IdentityAnchor)
}

// Flipping to the anchor already in use must not restart the service.
func TestIdentityAnchorNoOpLeavesRegistrationAlone(t *testing.T) {
	dataPath := t.TempDir()
	reg := approvedRegistration()
	reg.IdentityAnchor = registration.AnchorCloud
	dir := writeRegistration(t, dataPath, reg)

	before, err := os.Stat(filepath.Join(dir, "registration.json"))
	require.NoError(t, err)

	require.NoError(t, IdentityAnchor(anchorTestContext(t), IdentityAnchorOptions{
		Anchor:   registration.AnchorCloud,
		DataPath: dataPath,
	}))

	after, err := os.Stat(filepath.Join(dir, "registration.json"))
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "a no-op must not rewrite the registration")
}

// Startup lets an explicit setting outrank the registration, so a move made
// under a conflicting setting would report success and change nothing.
func TestIdentityAnchorRefusesWhenConfigOutranksIt(t *testing.T) {
	dataPath := t.TempDir()
	dir := writeRegistration(t, dataPath, approvedRegistration())

	t.Setenv("MIREN_WORKLOAD_IDENTITY_ANCHOR", registration.AnchorCluster)

	err := IdentityAnchor(anchorTestContext(t), IdentityAnchorOptions{
		Anchor:   registration.AnchorCloud,
		DataPath: dataPath,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "MIREN_WORKLOAD_IDENTITY_ANCHOR")
	require.Contains(t, err.Error(), "outranks the registration")

	// And the registration is left alone, so nothing claims a move happened.
	require.Empty(t, loadRegistration(t, dir).IdentityAnchor)
}

// A setting that agrees with the target is not a conflict.
func TestIdentityAnchorAllowsAgreeingConfig(t *testing.T) {
	dataPath := t.TempDir()
	dir := writeRegistration(t, dataPath, approvedRegistration())

	t.Setenv("MIREN_WORKLOAD_IDENTITY_ANCHOR", registration.AnchorCloud)

	require.NoError(t, IdentityAnchor(anchorTestContext(t), IdentityAnchorOptions{
		Anchor:   registration.AnchorCloud,
		DataPath: dataPath,
	}))
	require.Equal(t, registration.AnchorCloud, loadRegistration(t, dir).IdentityAnchor)
}

func TestIdentityAnchorRejects(t *testing.T) {
	tests := []struct {
		name    string
		anchor  string
		mutate  func(*registration.StoredRegistration)
		absent  bool
		wantErr string
	}{
		{
			name:    "unknown anchor",
			anchor:  "elsewhere",
			wantErr: "anchor must be",
		},
		{
			name:    "unregistered cluster",
			anchor:  registration.AnchorCloud,
			absent:  true,
			wantErr: "not registered with miren.cloud",
		},
		{
			name:    "cloud assigned no issuer",
			anchor:  registration.AnchorCloud,
			mutate:  func(r *registration.StoredRegistration) { r.IdentityIssuerURL = "" },
			wantErr: "has not assigned this cluster an identity issuer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataPath := t.TempDir()
			if !tt.absent {
				reg := approvedRegistration()
				if tt.mutate != nil {
					tt.mutate(reg)
				}
				writeRegistration(t, dataPath, reg)
			}

			err := IdentityAnchor(anchorTestContext(t), IdentityAnchorOptions{
				Anchor:   tt.anchor,
				DataPath: dataPath,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
