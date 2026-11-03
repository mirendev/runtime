package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

func diagnosticContext() *Context {
	return &Context{ClusterName: "local", CommandName: "route list"}
}

func TestRPCClientRoutingCredentialErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			t.Setenv(clientconfig.EnvConfigPath, dir)
			var signIns atomic.Int32
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/auth/refresh", r.URL.Path)
				signIns.Add(1)
				w.Header().Set("Retry-After", "20")
				http.Error(w, http.StatusText(status), status)
			}))
			defer cloud.Close()
			writeTokenIdentity(t, dir, cloud.URL)
			cfg, err := clientconfig.LoadConfig()
			require.NoError(t, err)
			identity, err := cfg.GetIdentity("cloud")
			require.NoError(t, err)
			identity.Token = "" // Force a refresh, both in memory and under the refresh lock.
			data, err := yaml.Marshal(clientconfig.ConfigData{Identities: map[string]*clientconfig.IdentityConfig{"cloud": identity}})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(cfg.GetIdentitySource("cloud"), data, 0600))
			silent, err := net.ListenPacket("udp", "127.0.0.1:0")
			require.NoError(t, err)
			defer silent.Close()
			cluster := &clientconfig.ClusterConfig{XID: "cluster-auth", Identity: "cloud", CloudURL: cloud.URL,
				Hostname: silent.LocalAddr().String(), AllAddresses: []string{silent.LocalAddr().String()}}
			ctx := &Context{Context: t.Context(), ClientConfig: cfg, ClusterConfig: cluster,
				ClusterName: "wilderness", Stderr: io.Discard, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			defer ctx.Close()
			_, err = ctx.RPCClient("entities")
			require.EqualValues(t, 1, signIns.Load(), "credential failures must not retry sign-in on another route")
			var diagnostic *ui.Diagnostic
			require.ErrorAs(t, err, &diagnostic)
			var rendered bytes.Buffer
			diagnostic.WriteForTerminal(&rendered)
			require.NotContains(t, rendered.String(), "miren doctor")
			if status == http.StatusTooManyRequests {
				require.Contains(t, rendered.String(), "too many sign-ins")
				require.Contains(t, rendered.String(), "waiting will")
				require.Contains(t, rendered.String(), "Retry in")
				require.NotContains(t, rendered.String(), "miren login")
				var limited *clientconfig.CloudRateLimitError
				require.ErrorAs(t, err, &limited)
			} else {
				require.Contains(t, rendered.String(), "miren login")
				require.ErrorIs(t, err, clientconfig.ErrLoginRequired)
			}
		})
	}
}

func TestRPCClientDiagnosesBothRoutesFailing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer cloud.Close()
	cfg := clientconfig.NewConfig()
	cfg.SetIdentity("cloud", &clientconfig.IdentityConfig{
		Type: clientconfig.IdentityToken, Issuer: cloud.URL, Token: freshLoginJWT(t),
	})
	cluster := &clientconfig.ClusterConfig{XID: "cluster-down", Identity: "cloud", CloudURL: cloud.URL,
		AllAddresses: []string{silent.LocalAddr().String()}, Insecure: true}
	ctx := &Context{Context: t.Context(), ClientConfig: cfg, ClusterConfig: cluster,
		ClusterName: "wilderness", CommandName: "app list", Stderr: io.Discard,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	defer ctx.Close()
	for range 2 {
		_, err := ctx.RPCClient("entities")
		d, ok := errors.AsType[*ui.Diagnostic](err)
		if !ok {
			t.Fatalf("expected a diagnostic for both failed routes, got %T: %v", err, err)
		}
		var rendered bytes.Buffer
		d.WriteForTerminal(&rendered)
		for _, want := range []string{`couldn't reach cluster "wilderness" directly or through Miren Cloud`,
			silent.LocalAddr().String(), "Nothing answered after", "404", "miren doctor"} {
			if !strings.Contains(rendered.String(), want) {
				t.Errorf("diagnostic lacks %q:\n%s", want, rendered.String())
			}
		}
		if _, ok := errors.AsType[*rpc.ResolveError](err); !ok {
			t.Error("the direct timeout must remain available to callers")
		}
	}
}

// Each kind has to produce its own diagnosis. The bug this replaces collapsed
// every transport failure into one message, so the regression to guard is two
// different failures rendering the same text.
func TestDiagnoseResolveDistinguishesKinds(t *testing.T) {
	c := diagnosticContext()

	tests := []struct {
		name        string
		err         error
		wantSummary string
		wantDetail  []string
	}{
		{
			name:        "unreachable",
			err:         rpc.NewResolveUnreachableError("entities", "localhost:8443", 5*time.Second, errors.New("timeout")),
			wantSummary: `couldn't reach cluster "local" at localhost:8443`,
			wantDetail:  []string{"Nothing answered after 5s"},
		},
		{
			name:        "went silent",
			err:         rpc.NewResolveWentSilentError("entities", "localhost:8443", 30*time.Second, errors.New("timeout")),
			wantSummary: `lost the connection to cluster "local" at localhost:8443`,
			wantDetail:  []string{"Connected successfully"},
		},
		{
			name:        "no answer",
			err:         rpc.NewResolveNoAnswerError("entities", "localhost:8443", 8*time.Second, errors.New("timeout")),
			wantSummary: `cluster "local" never answered`,
			wantDetail:  []string{"Waited 8s", "still open"},
		},
		{
			// The whole point of plumbing CommandName down: the user typed
			// "miren route list", not "entities".
			name:        "capability missing names the command, not the capability",
			err:         rpc.NewResolveLookupError("entities", "localhost:8443", "unknown object: entities"),
			wantSummary: `cluster "local" doesn't provide what "miren route list" needs`,
			wantDetail:  []string{"isn't currently offering"},
		},
		{
			name:        "unauthorized",
			err:         rpc.NewResolveStatusError("entities", "localhost:8443", 401),
			wantSummary: `access denied on cluster "local"`,
			wantDetail:  []string{"permission"},
		},
		{
			name:        "unexpected status",
			err:         rpc.NewResolveStatusError("entities", "localhost:8443", 404),
			wantSummary: `cluster "local" returned an unexpected response (HTTP 404)`,
			wantDetail:  []string{"isn't a miren server"},
		},
		{
			name:        "undecodable body",
			err:         rpc.NewResolveDecodeError("entities", "localhost:8443", errors.New("cbor: bad")),
			wantSummary: `couldn't understand the response from cluster "local"`,
			wantDetail:  []string{"expected format"},
		},
	}

	seen := map[string]string{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := c.wrapRPCError(tt.err)

			d, ok := errors.AsType[*ui.Diagnostic](wrapped)
			if !ok {
				t.Fatalf("wrapRPCError returned %T, want *ui.Diagnostic", wrapped)
			}
			if d.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", d.Summary, tt.wantSummary)
			}
			for _, want := range tt.wantDetail {
				if !strings.Contains(d.Detail, want) {
					t.Errorf("Detail %q does not mention %q", d.Detail, want)
				}
			}
			if prev, dup := seen[d.Summary]; dup {
				t.Errorf("%s renders the same summary as %s", tt.name, prev)
			}
			seen[d.Summary] = tt.name

			// Every diagnosis must leave the structured error reachable, since
			// doctor re-classifies failures handed to it.
			_, resolveOK := errors.AsType[*rpc.ResolveError](wrapped)
			if !resolveOK {
				t.Error("the underlying rpc.ResolveError is no longer reachable")
			}
		})
	}
}

// The 401 sentinel predates this change and is exported, so matching on it has
// to keep working through the diagnostic wrapper.
func TestUnauthorizedStillMatchesSentinel(t *testing.T) {
	c := diagnosticContext()
	wrapped := c.wrapRPCError(rpc.NewResolveStatusError("entities", "localhost:8443", 401))

	if !errors.Is(wrapped, ErrAccessDenied) {
		t.Error("errors.Is(err, ErrAccessDenied) no longer holds")
	}
}

// Without a command name (library callers, tests) the message should still be
// intelligible rather than containing an empty pair of quotes.
func TestDiagnoseResolveFallsBackToCapabilityName(t *testing.T) {
	c := &Context{ClusterName: "local"}
	wrapped := c.wrapRPCError(rpc.NewResolveLookupError("entities", "localhost:8443", "unknown object: entities"))

	d, ok := errors.AsType[*ui.Diagnostic](wrapped)
	if !ok {
		t.Fatalf("wrapRPCError returned %T, want *ui.Diagnostic", wrapped)
	}
	if !strings.Contains(d.Summary, `"entities"`) {
		t.Errorf("Summary = %q, want it to name the capability", d.Summary)
	}
}

// An unnamed cluster shouldn't produce 'cluster ""'.
func TestDiagnoseResolveHandlesUnnamedCluster(t *testing.T) {
	c := &Context{CommandName: "route list"}
	wrapped := c.wrapRPCError(rpc.NewResolveUnreachableError("entities", "localhost:8443", time.Second, errors.New("timeout")))

	d, ok := errors.AsType[*ui.Diagnostic](wrapped)
	if !ok {
		t.Fatalf("wrapRPCError returned %T, want *ui.Diagnostic", wrapped)
	}
	if strings.Contains(d.Summary, `cluster ""`) {
		t.Errorf("Summary = %q, want a readable fallback", d.Summary)
	}
	if !strings.Contains(d.Summary, "the cluster") {
		t.Errorf("Summary = %q, want it to fall back to 'the cluster'", d.Summary)
	}
}

// Unclassified transport failures should pass through untouched rather than
// being dressed up in a diagnosis we can't actually support.
func TestUnclassifiedTransportErrorPassesThrough(t *testing.T) {
	c := diagnosticContext()
	original := rpc.NewResolveHTTPError(errors.New("connection reset"), "error performing http request: %v", "connection reset")

	if got := c.wrapRPCError(original); got != original {
		t.Errorf("wrapRPCError modified an unclassified error: %v", got)
	}
}

// Non-RPC errors must pass through completely untouched.
func TestNonResolveErrorsPassThrough(t *testing.T) {
	c := diagnosticContext()
	original := errors.New("something else entirely")

	if got := c.wrapRPCError(original); got != original {
		t.Errorf("wrapRPCError modified an unrelated error: %v", got)
	}
}

// Naming a cluster we can't load has to be fatal. This used to fall through and
// answer with the *active* cluster's data, so `miren -C typo route list`
// happily printed production routes for a cluster that didn't exist.
func TestRPCClientRefusesUnknownCluster(t *testing.T) {
	c := &Context{
		Stderr:           io.Discard,
		clusterErr:       errors.New(`cluster "typo": failed to connect`),
		requestedCluster: "typo",
	}

	client, err := c.RPCClient("entities")
	if err == nil {
		t.Fatal("expected an error, got a client for a cluster that couldn't be loaded")
	}
	if client != nil {
		t.Error("no client should be returned")
	}

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !strings.Contains(d.Summary, "typo") {
		t.Errorf("Summary = %q, want it to name the requested cluster", d.Summary)
	}
	// The guard is that we rule out the silent substitution this path exists to
	// prevent. Deliberately not asserting "nothing was contacted": -C accepts an
	// ad-hoc address, which LoadCluster dials before failing, so that claim would
	// be false exactly when someone typo'd an address.
	if !strings.Contains(d.Detail, "didn't fall back") {
		t.Errorf("Detail = %q, want it to rule out falling back to another cluster", d.Detail)
	}
}

// The same protection applies when the broken name came from the config's
// active cluster rather than from -C.
func TestRPCClientRefusesBrokenActiveCluster(t *testing.T) {
	c := &Context{
		Stderr:     io.Discard,
		clusterErr: errors.New("active cluster is broken"),
	}

	err := func() error {
		_, err := c.RPCClient("entities")
		return err
	}()
	if err == nil {
		t.Fatal("expected an error for an unloadable active cluster")
	}

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !strings.Contains(d.Summary, "active cluster") {
		t.Errorf("Summary = %q, want it to blame the active cluster", d.Summary)
	}
}

// A cluster selected in the client config is just as authoritative as one
// loaded into ClusterConfig. This path used to log a warning and connect to the
// default server address instead, which is the same silent substitution the
// ClusterConfig path refuses to make — except it surfaced as a timeout against
// a server the user never named.
func TestRPCClientRefusesUnusableActiveCluster(t *testing.T) {
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("homelab", &clientconfig.ClusterConfig{
		Hostname: "homelab:8443",
		// A certificate with no key: enough to make the entry unusable.
		ClientCert: "-----BEGIN CERTIFICATE-----\nnot a real one\n-----END CERTIFICATE-----\n",
	})
	if err := cfg.SetActiveCluster("homelab"); err != nil {
		t.Fatal(err)
	}

	c := &Context{
		Context:      context.Background(),
		Stderr:       io.Discard,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		ClientConfig: cfg,
	}

	_, err := c.RPCClient("entities")
	if err == nil {
		t.Fatal("expected an error rather than a fallback to the default address")
	}

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !strings.Contains(d.Summary, "homelab") {
		t.Errorf("Summary = %q, want it to name the selected cluster", d.Summary)
	}
}

// Configured clusters with none selected is the state you land in after
// removing the active cluster. It used to fall through to the default address
// and fail as "no remote address specified", naming neither the problem nor the
// fix.
func TestRPCClientReportsNoActiveCluster(t *testing.T) {
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("cloud", &clientconfig.ClusterConfig{Hostname: "cloud.example.com:8443"})
	cfg.SetCluster("homelab", &clientconfig.ClusterConfig{Hostname: "homelab:8443"})
	// Active deliberately unset.

	c := &Context{
		Context:      context.Background(),
		Stderr:       io.Discard,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		ClientConfig: cfg,
	}

	_, err := c.RPCClient("entities")
	if err == nil {
		t.Fatal("expected an error when no cluster is active")
	}

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T (%v), want *ui.Diagnostic", err, err)
	}
	if !strings.Contains(d.Summary, "no active cluster") {
		t.Errorf("Summary = %q, want it to say nothing is active", d.Summary)
	}
	// The names are the thing you need to act on the advice.
	if len(d.Facts) == 0 || !strings.Contains(d.Facts[0].Value, "homelab") {
		t.Errorf("Facts = %v, want the configured cluster names", d.Facts)
	}
}

// A config with no clusters at all is a different thing from one with clusters
// and none active: it's local development before anything has been added, where
// the default address is what was meant. Refusing either would break `miren
// server` on localhost, so both shapes are pinned.
//
// They fail in different places, which is the trap. Only the nil case reaches
// the default-address fallback; an empty-but-present config stops earlier, at
// cs.Client, for want of an endpoint. Both are non-refusals, which is what this
// guards, but only one of them exercises the fall-through.
func TestRPCClientDoesNotRefuseWithoutClusters(t *testing.T) {
	newContext := func(cfg *clientconfig.Config) *Context {
		return &Context{
			Context:      context.Background(),
			Stderr:       io.Discard,
			Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			ClientConfig: cfg,
		}
	}

	// Whether a server answers the default address is not this test's business.
	assertNotRefused := func(t *testing.T, err error) {
		t.Helper()
		if d, ok := errors.AsType[*ui.Diagnostic](err); ok {
			t.Fatalf("refused instead of falling back: %s", d.Summary)
		}
	}

	t.Run("no config at all reaches the default address", func(t *testing.T) {
		c := newContext(nil)
		_, err := c.RPCClient("entities")
		assertNotRefused(t, err)
	})

	// The other half of the new guard: clusters must be present for the refusal
	// to fire, so an empty config must not trip it.
	t.Run("an empty config does not trip the no-active-cluster refusal", func(t *testing.T) {
		c := newContext(clientconfig.NewConfig())
		_, err := c.RPCClient("entities")
		assertNotRefused(t, err)
	})
}

// The underlying reason is the actionable part here, so it shows without -v.
func TestUnusableClusterErrorShowsItsCause(t *testing.T) {
	c := &Context{ClusterName: "prod"}
	err := c.unusableClusterError("prod", errors.New("tls: failed to find any PEM data"))

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !d.ShowCause {
		t.Error("the underlying reason should be visible without -v")
	}
	if !strings.Contains(d.Summary, "prod") {
		t.Errorf("Summary = %q, want it to name the cluster", d.Summary)
	}
}

// A rate-limited sign-in is not an expired one. Pointing at `miren login`
// would just spend another attempt against the same limit.
func TestUnusableClusterErrorRateLimited(t *testing.T) {
	c := &Context{ClusterName: "prod"}
	rl := &clientconfig.CloudRateLimitError{RetryAt: time.Now().Add(20 * time.Second)}
	err := c.unusableClusterError("prod", fmt.Errorf("failed to authenticate with cloud: %w", rl))

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	for _, a := range d.Actions {
		if strings.Contains(a.Command, "miren login") {
			t.Errorf("rate-limit diagnostic suggests %q", a.Command)
		}
	}
	if len(d.Facts) != 1 || d.Facts[0].Label != "Retry in" {
		t.Errorf("Facts = %+v, want a single Retry in", d.Facts)
	}
	if !errors.Is(err, rl) {
		t.Error("the rate-limit error should stay reachable via errors.Is")
	}
}
