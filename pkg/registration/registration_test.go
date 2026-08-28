package registration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The unattended (enroll-token) response fills in the cluster identity directly
// and carries no auth_url or poll_url. StartRegistration has to surface those
// fields and mark the result as registered so the caller skips polling.
func TestStartRegistrationDecodesRegisteredResponse(t *testing.T) {
	var gotBody Config
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/clusters/register/initiate", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		json.NewEncoder(w).Encode(Result{
			Status:           StatusRegistered,
			ClusterID:        "cluster-abc",
			OrganizationID:   "org-xyz",
			ServiceAccountID: "sa-123",
			DNSHostname:      "prod.example.miren.cloud",
			Tags:             map[string]string{"env": "prod"},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, Config{
		ClusterName: "prod",
		PublicKey:   "PUBKEY",
		EnrollToken: "met_testtoken",
	})

	result, err := client.StartRegistration(context.Background())
	require.NoError(t, err)

	assert.Equal(t, StatusRegistered, result.Status)
	assert.Equal(t, "cluster-abc", result.ClusterID)
	assert.Equal(t, "org-xyz", result.OrganizationID)
	assert.Equal(t, "sa-123", result.ServiceAccountID)
	assert.Equal(t, "prod.example.miren.cloud", result.DNSHostname)
	assert.Equal(t, map[string]string{"env": "prod"}, result.Tags)
	assert.Empty(t, result.AuthURL, "registered response carries no auth_url")
	assert.Empty(t, result.PollURL, "registered response carries no poll_url")

	assert.Equal(t, "met_testtoken", gotBody.EnrollToken, "token must be sent to cloud")
}

// PublicKeyFromPrivateKeyPEM must derive exactly the public key that
// GenerateKeyPair paired with the private key, so a retry reuses the same
// identity cloud already saw.
func TestPublicKeyFromPrivateKeyPEMRoundTrips(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	require.NoError(t, err)

	derived, err := PublicKeyFromPrivateKeyPEM(priv)
	require.NoError(t, err)
	assert.Equal(t, pub, derived)
}

func TestPublicKeyFromPrivateKeyPEMRejectsGarbage(t *testing.T) {
	_, err := PublicKeyFromPrivateKeyPEM("not a pem block")
	require.Error(t, err)
}

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
