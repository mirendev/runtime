package workloadidentity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeySetFingerprintIsStableAndChangesOnRotation(t *testing.T) {
	iss, err := NewIssuer(IssuerConfig{
		DataPath:       t.TempDir(),
		IssuerURL:      "https://cluster.example",
		OrganizationID: "org-1",
		ClusterID:      "cluster-1",
	})
	require.NoError(t, err)

	fingerprint := iss.KeySetFingerprint()
	require.NotEmpty(t, fingerprint)
	require.Equal(t, fingerprint, iss.KeySetFingerprint(), "same key set must fingerprint the same")

	// A different cluster's freshly generated key must not collide.
	other, err := NewIssuer(IssuerConfig{
		DataPath:       t.TempDir(),
		IssuerURL:      "https://cluster.example",
		OrganizationID: "org-1",
		ClusterID:      "cluster-1",
	})
	require.NoError(t, err)
	require.NotEqual(t, fingerprint, other.KeySetFingerprint())
}
