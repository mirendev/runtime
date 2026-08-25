package build

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	bkclient "github.com/moby/buildkit/client"
	bksecrets "github.com/moby/buildkit/session/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/secret"
)

// stubSecretResolver returns a fixed value per ref, or an error when the ref is
// unknown. It records how many times it was called (so a test can assert the
// resolver is left untouched when nothing is declared) and which backend each
// ref was resolved against (so a test can assert the backend is forwarded, and
// that an omitted backend defaults to "cluster").
type stubSecretResolver struct {
	values   map[string]string // ref -> plaintext
	backends map[string]string // ref -> backend seen on the resolve call
	calls    int
}

func (r *stubSecretResolver) ResolveRef(_ context.Context, backend, ref string) (secret.SecretValue, error) {
	r.calls++
	if r.backends == nil {
		r.backends = map[string]string{}
	}
	r.backends[ref] = backend
	v, ok := r.values[ref]
	if !ok {
		return secret.SecretValue{}, errors.New("no such secret")
	}
	return secret.SecretValue{Ref: ref, Bytes: []byte(v)}, nil
}

func newTestBuilder(secrets secret.Resolver) *Builder {
	return &Builder{
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Secrets: secrets,
	}
}

func buildSecretConfig(secrets ...appconfig.BuildSecret) *appconfig.AppConfig {
	return &appconfig.AppConfig{
		Name:  "test-app",
		Build: &appconfig.BuildConfig{Secrets: secrets},
	}
}

func TestResolveBuildSecretsResolvesEachEntry(t *testing.T) {
	r := require.New(t)
	resolver := &stubSecretResolver{values: map[string]string{
		"registry/npm-token": "npm-secret",
		"licenses/compiler":  "license-key",
	}}
	b := newTestBuilder(resolver)

	ac := buildSecretConfig(
		appconfig.BuildSecret{ID: "npm_token", Backend: "cluster", Ref: "registry/npm-token"},
		appconfig.BuildSecret{ID: "license", Backend: "cluster", Ref: "licenses/compiler"},
	)

	got, err := b.resolveBuildSecrets(context.Background(), ac, "dockerfile")
	r.NoError(err)
	r.Equal(map[string][]byte{
		"npm_token": []byte("npm-secret"),
		"license":   []byte("license-key"),
	}, got)
	// The declared backend must reach the resolver, not be dropped or defaulted
	// over.
	r.Equal("cluster", resolver.backends["registry/npm-token"])
}

func TestResolveBuildSecretsDefaultsBackendToCluster(t *testing.T) {
	r := require.New(t)
	resolver := &stubSecretResolver{values: map[string]string{
		"registry/npm-token": "npm-secret",
	}}
	b := newTestBuilder(resolver)

	// Backend omitted: it should resolve against the built-in cluster store.
	ac := buildSecretConfig(
		appconfig.BuildSecret{ID: "npm_token", Ref: "registry/npm-token"},
	)

	got, err := b.resolveBuildSecrets(context.Background(), ac, "dockerfile")
	r.NoError(err)
	r.Equal(map[string][]byte{"npm_token": []byte("npm-secret")}, got)
	r.Equal(secret.ClusterBackendName, resolver.backends["registry/npm-token"])
}

func TestResolveBuildSecretsRejectedForAutoStack(t *testing.T) {
	r := require.New(t)
	resolver := &stubSecretResolver{values: map[string]string{
		"registry/npm-token": "npm-secret",
	}}
	b := newTestBuilder(resolver)

	ac := buildSecretConfig(
		appconfig.BuildSecret{ID: "npm_token", Backend: "cluster", Ref: "registry/npm-token"},
	)

	// An auto-detected language stack has no place to consume the secret, so it
	// is rejected before the resolver is ever touched.
	_, err := b.resolveBuildSecrets(context.Background(), ac, "auto")
	r.Error(err)
	r.Contains(err.Error(), "only supported for Dockerfile builds")
	r.Contains(err.Error(), "npm_token")
	r.Zero(resolver.calls, "an auto-stack build must fail before resolving any secret")
}

func TestResolveBuildSecretsNilWhenNoneDeclared(t *testing.T) {
	r := require.New(t)
	resolver := &stubSecretResolver{}
	b := newTestBuilder(resolver)

	// nil config, nil build block, and an empty secrets list all short-circuit
	// before the resolver is touched.
	for _, ac := range []*appconfig.AppConfig{
		nil,
		{Name: "test-app"},
		buildSecretConfig(),
	} {
		// "auto" stack too: with nothing declared, the short-circuit wins and the
		// auto-stack guard never fires.
		got, err := b.resolveBuildSecrets(context.Background(), ac, "auto")
		r.NoError(err)
		r.Nil(got)
	}
	r.Zero(resolver.calls, "resolver must not be called when no build secrets are declared")
}

func TestResolveBuildSecretsErrorsWhenNoBackendsConfigured(t *testing.T) {
	r := require.New(t)
	b := newTestBuilder(nil) // cluster has no secret backends

	ac := buildSecretConfig(
		appconfig.BuildSecret{ID: "npm_token", Backend: "cluster", Ref: "registry/npm-token"},
	)

	_, err := b.resolveBuildSecrets(context.Background(), ac, "dockerfile")
	r.Error(err)
	r.Contains(err.Error(), "no secret backends configured")
	r.Contains(err.Error(), "npm_token")
}

func TestResolveBuildSecretsPropagatesResolverError(t *testing.T) {
	r := require.New(t)
	resolver := &stubSecretResolver{values: map[string]string{}} // knows nothing
	b := newTestBuilder(resolver)

	ac := buildSecretConfig(
		appconfig.BuildSecret{ID: "npm_token", Backend: "cluster", Ref: "registry/missing"},
	)

	_, err := b.resolveBuildSecrets(context.Background(), ac, "dockerfile")
	r.Error(err)
	r.Contains(err.Error(), "npm_token")
}

// The resolution logic above is only half the feature; these cover the wiring
// that actually carries the value to BuildKit — WithBuildSecrets stashing the
// map and attachBuildSecrets turning it into a session provider. The wiring is
// the security-critical half: it is what keeps the value on the session channel
// and out of the frontend attributes and image layers.

func TestWithBuildSecretsSetsField(t *testing.T) {
	r := require.New(t)
	m := map[string][]byte{"npm_token": []byte("npm-secret")}

	var opts transformOpt
	WithBuildSecrets(m)(&opts)

	r.Equal(m, opts.buildSecrets)
}

func TestAttachBuildSecretsNoopWhenEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		secrets map[string][]byte
	}{
		{"nil", nil},
		{"empty", map[string][]byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var solveOpt bkclient.SolveOpt
			attachBuildSecrets(&solveOpt, tc.secrets)
			assert.Empty(t, solveOpt.Session, "no session provider should be attached when there are no secrets")
		})
	}
}

// TestAttachBuildSecretsDeliversByID proves the attached provider hands the
// resolved value back keyed by its mount id, and answers NotFound for an id the
// build never declared. It stands the provider up over an in-memory gRPC
// connection, the same shape a real BuildKit session uses, so the test exercises
// the actual delivery path rather than reaching into the map.
func TestAttachBuildSecretsDeliversByID(t *testing.T) {
	r := require.New(t)

	var solveOpt bkclient.SolveOpt
	attachBuildSecrets(&solveOpt, map[string][]byte{"npm_token": []byte("npm-secret")})
	r.Len(solveOpt.Session, 1)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	solveOpt.Session[0].Register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	r.NoError(err)
	t.Cleanup(func() { conn.Close() })

	client := bksecrets.NewSecretsClient(conn)

	resp, err := client.GetSecret(context.Background(), &bksecrets.GetSecretRequest{ID: "npm_token"})
	r.NoError(err)
	r.Equal([]byte("npm-secret"), resp.Data)

	_, err = client.GetSecret(context.Background(), &bksecrets.GetSecretRequest{ID: "unknown"})
	r.Error(err)
	assert.Equal(t, codes.NotFound, status.Code(err), "an undeclared id must not resolve to some other secret")
}
