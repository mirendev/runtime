package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/registration"
)

// enrollCloud is a stub cloud for the unattended registration path. It only
// answers the initiate endpoint; a request to any other path (a poll, say)
// fails the test, which is how the "never fall back to interactive" guarantee
// is asserted.
type enrollCloud struct {
	server         *httptest.Server
	initiateStatus int               // HTTP status the initiate endpoint returns
	errorMessage   string            // error body when initiateStatus is not 200
	initiateCalls  int               // how many times initiate was hit
	lastPublicKey  string            // public_key from the most recent request
	lastToken      string            // enroll_token from the most recent request
	lastTags       map[string]string // tags from the most recent request
}

func newEnrollCloud(t *testing.T) *enrollCloud {
	t.Helper()

	c := &enrollCloud{initiateStatus: http.StatusOK}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clusters/register/initiate" {
			t.Errorf("unexpected request to %s (enroll path must not poll or fall back)", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var body registration.Config
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		c.initiateCalls++
		c.lastPublicKey = body.PublicKey
		c.lastToken = body.EnrollToken
		c.lastTags = body.Tags

		if c.initiateStatus != http.StatusOK {
			w.WriteHeader(c.initiateStatus)
			json.NewEncoder(w).Encode(map[string]string{"error": c.errorMessage})
			return
		}

		json.NewEncoder(w).Encode(registration.Result{
			Status:           registration.StatusRegistered,
			ClusterID:        "cluster-abc",
			OrganizationID:   "org-xyz",
			ServiceAccountID: "sa-123",
			DNSHostname:      "prod.example.miren.cloud",
			Tags:             body.Tags,
		})
	}))
	t.Cleanup(c.server.Close)

	return c
}

func testEnrollContext() (*Context, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Context{Context: context.Background(), Stdout: &buf}, &buf
}

// The happy path: a valid token registers the cluster in one request, writes an
// approved registration, and never polls.
func TestRegisterWithEnrollTokenSucceeds(t *testing.T) {
	cloud := newEnrollCloud(t)
	dir := t.TempDir()

	ctx, _ := testEnrollContext()
	err := Register(ctx, RegisterOptions{
		ClusterName: "prod",
		CloudURL:    cloud.server.URL,
		OutputDir:   dir,
		EnrollToken: "met_validtoken",
	})
	require.NoError(t, err)

	require.Equal(t, 1, cloud.initiateCalls)
	assert.Equal(t, "met_validtoken", cloud.lastToken)
	assert.NotEmpty(t, cloud.lastPublicKey)

	reg, err := registration.LoadRegistration(dir)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "approved", reg.Status)
	assert.Equal(t, "cluster-abc", reg.ClusterID)
	assert.Equal(t, "org-xyz", reg.OrganizationID)
	assert.Equal(t, "sa-123", reg.ServiceAccountID)
	assert.Equal(t, "prod.example.miren.cloud", reg.DNSHostname)
	assert.NotEmpty(t, reg.PrivateKey)
}

// A token cloud rejects is terminal: Register returns an error, the local state
// never reaches "approved", and nothing polls.
func TestRegisterWithEnrollTokenRejectedIsTerminal(t *testing.T) {
	cases := []struct {
		name   string
		status int
		msg    string
	}{
		{"already used", http.StatusConflict, "This enroll token has already been used"},
		{"expired", http.StatusGone, "This enroll token has expired"},
		{"revoked", http.StatusForbidden, "This enroll token has been revoked"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newEnrollCloud(t)
			cloud.initiateStatus = tc.status
			cloud.errorMessage = tc.msg
			dir := t.TempDir()

			ctx, _ := testEnrollContext()
			err := Register(ctx, RegisterOptions{
				ClusterName: "prod",
				CloudURL:    cloud.server.URL,
				OutputDir:   dir,
				EnrollToken: "met_badtoken",
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.msg)

			// The pre-request save leaves an "initializing" record; it must never
			// have advanced to "approved".
			reg, loadErr := registration.LoadRegistration(dir)
			require.NoError(t, loadErr)
			if reg != nil {
				assert.NotEqual(t, "approved", reg.Status)
			}
		})
	}
}

// A cloud that ignores the token and answers with the interactive shape must be
// treated as terminal too — an unattended machine has no browser to fall back
// to.
func TestRegisterWithEnrollTokenRefusesInteractiveFallback(t *testing.T) {
	dir := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/clusters/register/initiate", r.URL.Path)
		json.NewEncoder(w).Encode(registration.Result{
			Status:         registration.StatusPendingApproval,
			RegistrationID: "reg-1",
			AuthURL:        "https://miren.cloud/clusters/register/reg-1",
			PollURL:        ts0PollURL(r),
		})
	}))
	defer ts.Close()

	ctx, _ := testEnrollContext()
	err := Register(ctx, RegisterOptions{
		ClusterName: "prod",
		CloudURL:    ts.URL,
		OutputDir:   dir,
		EnrollToken: "met_ignoredtoken",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not honor the enroll token")
}

// ts0PollURL builds a poll URL on the same host, used only to make the
// interactive response well-formed. The test asserts we never actually poll it.
func ts0PollURL(r *http.Request) string {
	return "http://" + r.Host + "/api/v1/clusters/register/poll/nonce-1"
}

// A retry after an interrupted attempt must present the same public key so cloud
// can replay the original registration instead of refusing a spent token.
func TestRegisterWithEnrollTokenReusesSavedKeypairOnRetry(t *testing.T) {
	cloud := newEnrollCloud(t)
	dir := t.TempDir()

	// Seed the state a crashed first attempt would leave: the keypair saved,
	// status still "initializing".
	priv, _, err := registration.GenerateKeyPair()
	require.NoError(t, err)
	require.NoError(t, registration.SaveRegistration(dir, &registration.StoredRegistration{
		ClusterName: "prod",
		PrivateKey:  priv,
		CloudURL:    cloud.server.URL,
		Status:      "initializing",
	}))

	expectedPub, err := registration.PublicKeyFromPrivateKeyPEM(priv)
	require.NoError(t, err)

	ctx, _ := testEnrollContext()
	err = Register(ctx, RegisterOptions{
		ClusterName: "prod",
		CloudURL:    cloud.server.URL,
		OutputDir:   dir,
		EnrollToken: "met_validtoken",
	})
	require.NoError(t, err)

	assert.Equal(t, expectedPub, cloud.lastPublicKey, "retry must reuse the saved keypair")

	reg, err := registration.LoadRegistration(dir)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "approved", reg.Status)
	assert.Equal(t, priv, reg.PrivateKey, "the saved private key must be kept")
}
