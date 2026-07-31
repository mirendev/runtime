package runnertelemetry_test

import (
	"errors"
	"testing"
	"time"

	"miren.dev/runtime/pkg/caauth"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/runnertelemetry"
)

// stubIssuer counts mints so token caching can be observed, and records the
// options it was asked for.
type stubIssuer struct {
	mints    int
	err      error
	gotOpts  workloadidentity.TokenOptions
	gotLoad  workloadidentity.SystemWorkload
	tokenSeq []string
}

func (s *stubIssuer) IssueToken(app, sandboxID string) (string, error) { return "", nil }

func (s *stubIssuer) IssueTokenWithOptions(app, sandboxID string, opts workloadidentity.TokenOptions) (string, error) {
	return "", nil
}

func (s *stubIssuer) IssuerURL() string { return "https://issuer.invalid" }

func (s *stubIssuer) IssueSystemWorkloadToken(workload workloadidentity.SystemWorkload, opts workloadidentity.TokenOptions) (string, error) {
	s.gotLoad, s.gotOpts = workload, opts
	if s.err != nil {
		return "", s.err
	}
	s.mints++
	if len(s.tokenSeq) >= s.mints {
		return s.tokenSeq[s.mints-1], nil
	}
	return "token", nil
}

// Until the runner has connected there is no issuer, and the honest answer is a
// failure. Returning an empty token instead would have the coordinator reject
// the batch, which looks like a credential problem rather than a startup
// ordering one.
func TestTokenSourceFailsBeforeIssuerArrives(t *testing.T) {
	src := runnertelemetry.NewIssuerTokenSource()

	_, err := src.Token()
	require.ErrorIs(t, err, runnertelemetry.ErrIssuerUnavailable)
}

func TestTokenSourceMintsAndCaches(t *testing.T) {
	iss := &stubIssuer{}
	src := runnertelemetry.NewIssuerTokenSource()
	src.SetIssuer(iss)

	first, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "token", first)

	second, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, iss.mints, "a live token should be reused rather than reminted per request")

	// The audience is what keeps this token from being spendable at another
	// service, and the explicit TTL is what lets the source know when to renew
	// without decoding the token.
	require.Equal(t, workloadidentity.SystemWorkloadTelemetryWriter, iss.gotLoad)
	require.Equal(t, []string{runnertelemetry.Audience}, iss.gotOpts.Audience)
	require.NotZero(t, iss.gotOpts.TTL)
}

func TestTokenSourcePropagatesMintFailure(t *testing.T) {
	iss := &stubIssuer{err: errors.New("coordinator refused")}
	src := runnertelemetry.NewIssuerTokenSource()
	src.SetIssuer(iss)

	_, err := src.Token()
	require.Error(t, err)
	require.Contains(t, err.Error(), "coordinator refused")
}

// Re-arming drops the cached token. A new issuer means a new connection to the
// coordinator, and holding a token minted through the old one would keep a
// stale credential alive past the event that replaced it.
func TestTokenSourceResetsOnNewIssuer(t *testing.T) {
	iss := &stubIssuer{tokenSeq: []string{"first", "second"}}
	src := runnertelemetry.NewIssuerTokenSource()

	src.SetIssuer(iss)
	first, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "first", first)

	src.SetIssuer(iss)
	second, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "second", second)
	require.Equal(t, 2, iss.mints)
}

func TestClientRequiresTokenSource(t *testing.T) {
	_, err := runnertelemetry.NewClient(runnertelemetry.ClientConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "token source")
}

func TestURLsComposeWithWriterSuffixes(t *testing.T) {
	require.Equal(t, "https://coordinator.invalid:8443/_telemetry/metrics",
		runnertelemetry.MetricsURL("coordinator.invalid:8443"))
	require.Equal(t, "https://coordinator.invalid:8443/_telemetry/logs",
		runnertelemetry.LogsURL("coordinator.invalid:8443"))
}

// The QUIC transport underneath the client has to be reachable for closing.
// Left anonymous inside NewClient it would hold connections open until the
// process exited, which is why Client hands back a handle rather than a bare
// *http.Client.
func TestClientClosesItsTransport(t *testing.T) {
	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "miren", ValidFor: time.Hour})
	require.NoError(t, err)

	runnerCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "runner-abc",
		Organization: "miren",
		ValidFor:     time.Hour,
	})
	require.NoError(t, err)

	src := runnertelemetry.NewIssuerTokenSource()
	src.SetIssuer(&stubIssuer{})

	client, err := runnertelemetry.NewClient(runnertelemetry.ClientConfig{
		ClientCertPEM: runnerCert.CertPEM,
		ClientKeyPEM:  runnerCert.KeyPEM,
		CACertPEM:     ca.GetCACertificate(),
		TokenSource:   src,
	})
	require.NoError(t, err)
	require.NotNil(t, client.HTTP)

	require.NoError(t, client.Close())

	// Shutdown paths call this from a defer that may run more than once.
	require.NoError(t, client.Close())
}

func TestNilClientCloseIsSafe(t *testing.T) {
	var c *runnertelemetry.Client
	require.NoError(t, c.Close())
}

func TestClientRejectsUnparseableCA(t *testing.T) {
	src := runnertelemetry.NewIssuerTokenSource()
	src.SetIssuer(&stubIssuer{})

	_, err := runnertelemetry.NewClient(runnertelemetry.ClientConfig{
		ClientCertPEM: []byte("not a cert"),
		ClientKeyPEM:  []byte("not a key"),
		CACertPEM:     []byte("not a ca"),
		TokenSource:   src,
	})
	require.Error(t, err)
}
