package coordinate

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/registration"
)

const reportedAnchor = "https://api.miren.cloud/identity/cluster-abc"

func registeredCloudControl(t *testing.T, stored string) (*CloudControl, string) {
	t.Helper()

	dataPath := t.TempDir()
	dir := filepath.Join(dataPath, "server")
	require.NoError(t, registration.SaveRegistration(dir, &registration.StoredRegistration{
		ClusterID:         "cluster-abc",
		ClusterName:       "test",
		Status:            "approved",
		IdentityIssuerURL: stored,
		PrivateKey:        "unused",
	}))

	c := NewCloudControl(&Foundation{Log: testLogger()})
	c.DataPath = dataPath
	c.CloudAuth = CloudAuthConfig{Enabled: true, ClusterID: "cluster-abc", IdentityIssuerURL: stored}
	return c, dir
}

// The case this exists for: a cluster registered before anchors existed has
// none recorded, and without learning one from a status report it could never
// be moved to the cloud anchor without re-registering.
func TestRecordIdentityAnchorPersistsForAPreexistingRegistration(t *testing.T) {
	c, dir := registeredCloudControl(t, "")

	c.recordIdentityAnchor(reportedAnchor)

	reg, err := registration.LoadRegistration(dir)
	require.NoError(t, err)
	require.Equal(t, reportedAnchor, reg.IdentityIssuerURL)
	require.Equal(t, reportedAnchor, c.CloudAuth.IdentityIssuerURL)

	// Recording is not adopting: the anchor a cluster mints under is decided at
	// startup, so this only makes the move available.
	require.Empty(t, reg.IdentityAnchor)
}

func TestRecordIdentityAnchorIgnoresNoOps(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		reported string
	}{
		{name: "cloud serves no discovery", stored: "", reported: ""},
		{name: "unchanged", stored: reportedAnchor, reported: reportedAnchor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, dir := registeredCloudControl(t, tt.stored)
			before, err := registration.LoadRegistration(dir)
			require.NoError(t, err)

			c.recordIdentityAnchor(tt.reported)

			after, err := registration.LoadRegistration(dir)
			require.NoError(t, err)
			require.Equal(t, before.IdentityIssuerURL, after.IdentityIssuerURL)
		})
	}
}

// An unregistered cluster has no file to write, and must not gain one.
func TestRecordIdentityAnchorOnUnregisteredClusterIsInert(t *testing.T) {
	dataPath := t.TempDir()
	c := NewCloudControl(&Foundation{Log: testLogger()})
	c.DataPath = dataPath

	require.NotPanics(t, func() { c.recordIdentityAnchor(reportedAnchor) })

	reg, err := registration.LoadRegistration(filepath.Join(dataPath, "server"))
	require.NoError(t, err)
	require.Nil(t, reg)
}
