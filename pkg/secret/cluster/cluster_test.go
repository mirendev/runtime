package cluster

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/secret/keyring"
	server "miren.dev/runtime/servers/entityserver"
)

// newTestBackend stands up a real etcd-backed entity store behind the entity
// server, because resolving a version by its short id goes through the server's
// short-id index — the part a mock store cannot exercise.
func newTestBackend(t *testing.T) (*Backend, *entityserver.Client) {
	t.Helper()

	log := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(ctx, log, client, prefix)
	require.NoError(t, err)
	require.NoError(t, schema.Apply(ctx, store))

	eac := esv1.NewEntityAccessClient(rpc.LocalClient(
		esv1.AdaptEntityAccess(&server.EntityServer{Log: log, Store: store}),
	))
	ec := entityserver.NewClient(log, eac)

	ring, err := keyring.Generate()
	require.NoError(t, err)

	return NewBackend(log, ec, ring), ec
}

func TestPutThenResolveRoundTrip(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	value := []byte("sk_live_abc123")
	version, err := b.Put(ctx, "payments/stripe-key", value)
	require.NoError(t, err)
	require.NotEmpty(t, version)

	got, err := b.Resolve(ctx, "payments/stripe-key")
	require.NoError(t, err)
	assert.Equal(t, value, got.Bytes)

	// A floating reference must come back fully qualified, so whatever records
	// it freezes an exact version.
	assert.Equal(t, secret.FormatRef("payments/stripe-key", version), got.Ref)
}

func TestResolvePinnedVersionSurvivesRotation(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	first, err := b.Put(ctx, "payments/stripe-key", []byte("old"))
	require.NoError(t, err)

	second, err := b.Put(ctx, "payments/stripe-key", []byte("new"))
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	// The pin still resolves to the bytes it was pinned at — this is what makes
	// a rollback restore the secret the old config actually shipped with.
	pinned, err := b.Resolve(ctx, secret.FormatRef("payments/stripe-key", first))
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), pinned.Bytes)

	floating, err := b.Resolve(ctx, "payments/stripe-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), floating.Bytes)
	assert.Equal(t, secret.FormatRef("payments/stripe-key", second), floating.Ref)
}

// Re-running the same `secret set` must not churn a version and invalidate
// every pin, so an identical value is recognized without decrypting anything.
func TestPutIsANoOpForAnIdenticalValue(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	first, err := b.Put(ctx, "payments/stripe-key", []byte("same"))
	require.NoError(t, err)

	second, err := b.Put(ctx, "payments/stripe-key", []byte("same"))
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestPutMintsANewVersionForAChangedValue(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	first, err := b.Put(ctx, "payments/stripe-key", []byte("old"))
	require.NoError(t, err)

	second, err := b.Put(ctx, "payments/stripe-key", []byte("new"))
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

// Nothing sensitive may be readable from the store itself: that is the entire
// reason a cluster reference beats an inline literal.
func TestStoredVersionHoldsNoPlaintext(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()

	value := []byte("sk_live_abc123")
	_, err := b.Put(ctx, "payments/stripe-key", value)
	require.NoError(t, err)

	var sec core_v1alpha.Secret
	require.NoError(t, ec.Get(ctx, "payments.stripe-key", &sec))
	require.NotEmpty(t, sec.CurrentVersion)

	var sv core_v1alpha.SecretVersion
	require.NoError(t, ec.GetById(ctx, sec.CurrentVersion, &sv))

	assert.NotContains(t, string(sv.Ciphertext), string(value))
	assert.NotContains(t, string(sv.WrappedDek), string(value))
	assert.NotContains(t, sv.ValueMac, string(value))
	assert.NotEmpty(t, sv.KekId)
}

func TestResolveFailsClosedOnADisabledVersion(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	version, err := b.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	ref := secret.FormatRef("payments/stripe-key", version)
	require.NoError(t, b.SetState(ctx, ref, secret.StateDisabled))

	_, err = b.Resolve(ctx, ref)
	assert.ErrorIs(t, err, secret.ErrVersionNotEnabled)

	// The floating reference still points at it, so it fails closed too rather
	// than falling back to an older enabled version.
	_, err = b.Resolve(ctx, "payments/stripe-key")
	assert.ErrorIs(t, err, secret.ErrVersionNotEnabled)
}

func TestDestroyRemovesThePayload(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()

	version, err := b.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	ref := secret.FormatRef("payments/stripe-key", version)
	require.NoError(t, b.SetState(ctx, ref, secret.StateDestroyed))

	var sec core_v1alpha.Secret
	require.NoError(t, ec.Get(ctx, "payments.stripe-key", &sec))

	var sv core_v1alpha.SecretVersion
	require.NoError(t, ec.GetById(ctx, sec.CurrentVersion, &sv))

	assert.Equal(t, core_v1alpha.DESTROYED, sv.State)
	assert.Empty(t, sv.Ciphertext)
	assert.Empty(t, sv.WrappedDek)
}

// Re-enabling a disabled version has to bring back the same bytes, since the
// state transition never touched the payload.
func TestSetStateCanReEnableADisabledVersion(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	version, err := b.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)
	ref := secret.FormatRef("payments/stripe-key", version)

	require.NoError(t, b.SetState(ctx, ref, secret.StateDisabled))
	require.NoError(t, b.SetState(ctx, ref, secret.StateEnabled))

	got, err := b.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("sk_live"), got.Bytes)
}

func TestSetStateNeedsAConcreteVersion(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	_, err := b.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	err = b.SetState(ctx, "payments/stripe-key", secret.StateDisabled)
	assert.Error(t, err)
}

// Version handles are globally unique short ids, so a reference could name a
// version belonging to a different secret. It must not hand back those bytes.
func TestResolveRejectsAVersionFromAnotherSecret(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	_, err := b.Put(ctx, "payments/stripe-key", []byte("stripe"))
	require.NoError(t, err)

	otherVersion, err := b.Put(ctx, "registry/npm-token", []byte("npm"))
	require.NoError(t, err)

	_, err = b.Resolve(ctx, secret.FormatRef("payments/stripe-key", otherVersion))
	assert.ErrorIs(t, err, secret.ErrNotFound)
}

func TestResolveUnknownSecret(t *testing.T) {
	b, _ := newTestBackend(t)

	_, err := b.Resolve(t.Context(), "nothing/here")
	assert.ErrorIs(t, err, secret.ErrNotFound)
}

func TestResolveUnknownVersion(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	_, err := b.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	_, err = b.Resolve(ctx, "payments/stripe-key@nope")
	assert.ErrorIs(t, err, secret.ErrNotFound)
}

func TestPutRejectsAnInvalidPath(t *testing.T) {
	b, _ := newTestBackend(t)

	_, err := b.Put(t.Context(), "payments/../etc", []byte("x"))
	assert.Error(t, err)
}

// Two secrets whose paths differ only by separator must not collide on one
// entity, which is why paths exclude the character the entity name uses.
func TestPathsDifferingBySeparatorStaySeparate(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := t.Context()

	_, err := b.Put(ctx, "a/b", []byte("slash"))
	require.NoError(t, err)

	// "a.b" is not an addressable path, so it cannot alias "a/b".
	_, err = b.Put(ctx, "a.b", []byte("dot"))
	assert.Error(t, err)

	got, err := b.Resolve(ctx, "a/b")
	require.NoError(t, err)
	assert.Equal(t, []byte("slash"), got.Bytes)
}

// The registry treats the cluster backend as writable, which is what
// distinguishes a store Miren owns from an external manager it only reads.
func TestBackendRegistersAsWritable(t *testing.T) {
	b, _ := newTestBackend(t)

	reg := secret.NewRegistry()
	reg.Register(b)

	assert.Equal(t, secret.ClusterBackendName, b.Name())

	w, err := reg.Writable(secret.ClusterBackendName)
	require.NoError(t, err)
	assert.NotNil(t, w)
}

func TestValidatePath(t *testing.T) {
	valid := []string{
		"token",
		"payments/stripe-key",
		"a/b/c",
		"Mixed_Case-123/seg2",
	}
	for _, path := range valid {
		assert.NoError(t, ValidatePath(path), path)
	}

	invalid := []string{
		"",
		"/leading",
		"trailing/",
		"double//slash",
		"has space",
		"has.dot",
		"-leading-hyphen",
		"unicode/ünicode",
	}
	for _, path := range invalid {
		assert.Error(t, ValidatePath(path), path)
	}
}

func TestEntityNameIsInjective(t *testing.T) {
	assert.Equal(t, "payments.stripe-key", entityName("payments/stripe-key"))
	assert.Equal(t, "token", entityName("token"))
}
