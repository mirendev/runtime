package runnertelemetry_test

import (
	"io"
	"net/http"
	"strings"
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

// The runner's source has to mint telemetry writer tokens for the ingest
// audience; the coordinator verifies both before accepting a batch.
func TestTokenSourceScopesToIngest(t *testing.T) {
	iss := &stubIssuer{}
	src := runnertelemetry.NewTokenSource()
	src.SetIssuer(iss)

	_, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, workloadidentity.SystemWorkloadTelemetryWriter, iss.gotLoad)
	require.Equal(t, []string{runnertelemetry.Audience}, iss.gotOpts.Audience)
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

	src := runnertelemetry.NewTokenSource()
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
	src := runnertelemetry.NewTokenSource()
	src.SetIssuer(&stubIssuer{})

	_, err := runnertelemetry.NewClient(runnertelemetry.ClientConfig{
		ClientCertPEM: []byte("not a cert"),
		ClientKeyPEM:  []byte("not a key"),
		CACertPEM:     []byte("not a ca"),
		TokenSource:   src,
	})
	require.Error(t, err)
}

type recordingTransport struct {
	gotStream []string
}

func (t *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.gotStream = append(t.gotStream, r.Header.Get(runnertelemetry.StreamHeader))
	return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

// The operational client is how the coordinator tells a runner's own series
// from its sandboxes', so it has to mark every request, and the ordinary
// client it was derived from must stay unmarked.
func TestOperationalClientMarksItsRequests(t *testing.T) {
	transport := &recordingTransport{}
	client := &runnertelemetry.Client{HTTP: &http.Client{Transport: transport}}

	resp, err := client.Operational().Post("https://coordinator.invalid/x", "text/plain", strings.NewReader("m 1 1\n"))
	require.NoError(t, err)
	resp.Body.Close()

	resp, err = client.HTTP.Post("https://coordinator.invalid/x", "text/plain", strings.NewReader("m 1 1\n"))
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, []string{runnertelemetry.StreamOperational, ""}, transport.gotStream)
}
