package secret

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/secret/cluster"
	"miren.dev/runtime/pkg/secret/keyring"
	entityserversrv "miren.dev/runtime/servers/entityserver"
)

// newTestClient stands up the secrets service over a real etcd-backed store and
// returns a client speaking to it, so the tests exercise the same path the CLI
// takes.
func newTestClient(t *testing.T) *secret_v1alpha.SecretsClient {
	t.Helper()

	log := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(ctx, log, client, prefix)
	require.NoError(t, err)
	require.NoError(t, schema.Apply(ctx, store))

	eac := esv1.NewEntityAccessClient(rpc.LocalClient(
		esv1.AdaptEntityAccess(&entityserversrv.EntityServer{Log: log, Store: store}),
	))
	ec := entityserver.NewClient(log, eac)

	ring, err := keyring.Generate()
	require.NoError(t, err)

	registry := secret.NewRegistry()
	registry.Register(cluster.NewBackend(log, ec, ring))

	return secret_v1alpha.NewSecretsClient(rpc.LocalClient(
		secret_v1alpha.AdaptSecrets(NewServer(log, registry)),
	))
}

// An empty backend falls back to the built-in cluster store, so the common case
// needs no --backend flag.
func TestSetDefaultsToTheClusterBackend(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	res, err := c.Set(ctx, "", "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)
	assert.NotEmpty(t, res.Version())
	assert.False(t, res.Unchanged())

	listed, err := c.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, listed.Secrets(), 1)
	assert.Equal(t, secret.ClusterBackendName, listed.Secrets()[0].Backend())
}

// Re-running the same `secret set` must report that nothing moved, rather than
// implying a rotation that would need propagating.
func TestSetReportsAnUnchangedValue(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	first, err := c.Set(ctx, "", "payments/stripe-key", []byte("same"))
	require.NoError(t, err)
	assert.False(t, first.Unchanged())

	second, err := c.Set(ctx, "", "payments/stripe-key", []byte("same"))
	require.NoError(t, err)
	assert.True(t, second.Unchanged())
	assert.Equal(t, first.Version(), second.Version())

	third, err := c.Set(ctx, "", "payments/stripe-key", []byte("different"))
	require.NoError(t, err)
	assert.False(t, third.Unchanged())
	assert.NotEqual(t, first.Version(), third.Version())
}

func TestSetRejectsAnEmptyValue(t *testing.T) {
	c := newTestClient(t)

	_, err := c.Set(t.Context(), "", "payments/stripe-key", nil)
	assert.Error(t, err)
}

func TestSetRejectsAnUnknownBackend(t *testing.T) {
	c := newTestClient(t)

	_, err := c.Set(t.Context(), "prod-vault", "payments/stripe-key", []byte("x"))
	assert.Error(t, err)
}

func TestListVersionsMarksTheCurrentVersion(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	first, err := c.Set(ctx, "", "payments/stripe-key", []byte("old"))
	require.NoError(t, err)
	second, err := c.Set(ctx, "", "payments/stripe-key", []byte("new"))
	require.NoError(t, err)

	res, err := c.ListVersions(ctx, "", "payments/stripe-key")
	require.NoError(t, err)

	info := res.Secret()
	assert.Equal(t, "payments/stripe-key", info.Path())
	assert.Equal(t, second.Version(), info.CurrentVersion())
	require.Len(t, info.Versions(), 2)

	current := map[string]bool{}
	for _, v := range info.Versions() {
		current[v.Version()] = v.Current()
		assert.Equal(t, string(secret.StateEnabled), v.State())
		assert.NotZero(t, v.CreatedAt())
	}
	assert.True(t, current[second.Version()])
	assert.False(t, current[first.Version()])
}

func TestSetStateDisablesAVersion(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	set, err := c.Set(ctx, "", "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	ref := secret.FormatRef("payments/stripe-key", set.Version())
	_, err = c.SetState(ctx, "", ref, string(secret.StateDisabled))
	require.NoError(t, err)

	res, err := c.ListVersions(ctx, "", "payments/stripe-key")
	require.NoError(t, err)
	require.Len(t, res.Secret().Versions(), 1)
	assert.Equal(t, string(secret.StateDisabled), res.Secret().Versions()[0].State())
}

func TestSetStateRejectsAnUnknownState(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	set, err := c.Set(ctx, "", "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	ref := secret.FormatRef("payments/stripe-key", set.Version())
	_, err = c.SetState(ctx, "", ref, "revoked")
	assert.Error(t, err)
}

func TestListReturnsSecretsSortedByPath(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	for _, path := range []string{"registry/npm-token", "payments/stripe-key", "auth/session-key"} {
		_, err := c.Set(ctx, "", path, []byte("value-for-"+path))
		require.NoError(t, err)
	}

	res, err := c.List(ctx, "")
	require.NoError(t, err)

	paths := make([]string, 0, len(res.Secrets()))
	for _, s := range res.Secrets() {
		paths = append(paths, s.Path())
	}
	assert.Equal(t, []string{"auth/session-key", "payments/stripe-key", "registry/npm-token"}, paths)
}

// Nothing the service returns may carry a value — listing a secret is an
// operator's view of blast radius, not a way to read it back.
func TestListingNeverCarriesAValue(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	const value = "sk_live_abc123"
	_, err := c.Set(ctx, "", "payments/stripe-key", []byte(value))
	require.NoError(t, err)

	listed, err := c.List(ctx, "")
	require.NoError(t, err)
	for _, s := range listed.Secrets() {
		assert.NotContains(t, s.Path(), value)
		assert.NotContains(t, s.CurrentVersion(), value)
		for _, v := range s.Versions() {
			assert.NotContains(t, v.Version(), value)
			assert.NotContains(t, v.State(), value)
		}
	}
}

func TestListVersionsOnAnUnknownSecret(t *testing.T) {
	c := newTestClient(t)

	_, err := c.ListVersions(t.Context(), "", "nothing/here")
	assert.Error(t, err)
}
