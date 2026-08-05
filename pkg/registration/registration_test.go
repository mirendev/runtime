package registration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearRegistration(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, SaveRegistration(dir, &StoredRegistration{
		ClusterID:   "cluster-123",
		ClusterName: "prod",
		PrivateKey:  "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n",
		Status:      "approved",
	}))

	// Everything else in the server directory belongs to something other than
	// cloud registration and has to survive: these secure CLI-to-cluster auth,
	// end-user OIDC sessions, and the cluster's own workload identities.
	bystanders := []string{"ca.crt", "ca.key", "api.crt", "api.key", "oidc-signing.key", "workload-identity.key"}
	for _, name := range bystanders {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("keep me"), 0600))
	}

	require.NoError(t, ClearRegistration(dir))

	reg, err := LoadRegistration(dir)
	require.NoError(t, err)
	assert.Nil(t, reg)

	for _, name := range RegistrationFiles {
		assert.NoFileExists(t, filepath.Join(dir, name))
	}
	for _, name := range bystanders {
		assert.FileExists(t, filepath.Join(dir, name))
	}
}

// Clearing a directory that was already cleared should succeed, so an
// interrupted unregister can be finished by running it again.
func TestClearRegistrationIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, ClearRegistration(dir))
	require.NoError(t, ClearRegistration(dir))
}

// A partially cleared directory — registration.json gone but the key left
// behind — should finish cleanly rather than stopping at the missing file.
func TestClearRegistrationPartial(t *testing.T) {
	dir := t.TempDir()

	keyPath := filepath.Join(dir, "service-account.key")
	require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0600))

	require.NoError(t, ClearRegistration(dir))

	assert.NoFileExists(t, keyPath)
}
