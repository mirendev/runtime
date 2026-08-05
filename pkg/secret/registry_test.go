package secret

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readOnlyBackend stands in for an external manager: it resolves, but Miren has
// no way to write into it.
type readOnlyBackend struct {
	name   string
	values map[string][]byte
}

func (b *readOnlyBackend) Name() string { return b.name }

func (b *readOnlyBackend) Resolve(ctx context.Context, ref string) (SecretValue, error) {
	path, version, err := ParseRef(ref)
	if err != nil {
		return SecretValue{}, err
	}
	if version == "" {
		version = "current"
	}
	val, ok := b.values[path]
	if !ok {
		return SecretValue{}, ErrNotFound
	}
	return SecretValue{Ref: FormatRef(path, version), Bytes: val}, nil
}

// writableBackend stands in for a backend Miren manages.
type writableBackend struct {
	readOnlyBackend
}

func (b *writableBackend) Put(ctx context.Context, path string, value []byte) (string, bool, error) {
	_, reused := b.values[path]
	b.values[path] = value
	return "v1", reused, nil
}

func (b *writableBackend) SetState(ctx context.Context, ref string, state VersionState) error {
	return nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	b := &readOnlyBackend{name: "prod-vault"}
	r.Register(b)

	got, ok := r.Get("prod-vault")
	require.True(t, ok)
	assert.Equal(t, b, got)

	_, ok = r.Get("staging-vault")
	assert.False(t, ok)
}

func TestRegistryNamesAreSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(&readOnlyBackend{name: "prod-vault"})
	r.Register(&readOnlyBackend{name: ClusterBackendName})
	r.Register(&readOnlyBackend{name: "aws-sm"})

	assert.Equal(t, []string{"aws-sm", ClusterBackendName, "prod-vault"}, r.Names())
}

// The read-only split is the type-level form of "reference, don't copy": there
// is no path by which Miren writes into an external manager.
func TestRegistryWritableRejectsReadOnlyBackend(t *testing.T) {
	r := NewRegistry()
	r.Register(&readOnlyBackend{name: "prod-vault"})

	_, err := r.Writable("prod-vault")
	assert.ErrorIs(t, err, ErrReadOnlyBackend)
}

func TestRegistryWritableReturnsManagedBackend(t *testing.T) {
	r := NewRegistry()
	b := &writableBackend{readOnlyBackend{name: ClusterBackendName, values: map[string][]byte{}}}
	r.Register(b)

	w, err := r.Writable(ClusterBackendName)
	require.NoError(t, err)

	version, reused, err := w.Put(context.Background(), "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)
	assert.Equal(t, "v1", version)
	assert.False(t, reused)
}

func TestRegistryWritableUnknownBackend(t *testing.T) {
	r := NewRegistry()
	_, err := r.Writable("nope")
	assert.ErrorIs(t, err, ErrUnknownBackend)
}

func TestRegistryResolveRef(t *testing.T) {
	r := NewRegistry()
	r.Register(&readOnlyBackend{
		name:   ClusterBackendName,
		values: map[string][]byte{"payments/stripe-key": []byte("sk_live")},
	})

	val, err := r.ResolveRef(context.Background(), ClusterBackendName, "payments/stripe-key@x1A")
	require.NoError(t, err)
	assert.Equal(t, "payments/stripe-key@x1A", val.Ref)
	assert.Equal(t, []byte("sk_live"), val.Bytes)
}

func TestRegistryResolveRefUnknownBackend(t *testing.T) {
	r := NewRegistry()
	_, err := r.ResolveRef(context.Background(), "prod-vault", "secret/data/app")
	assert.ErrorIs(t, err, ErrUnknownBackend)
}

// A resolution failure has to name what could not be resolved without quoting
// the value or the store's own error text back to the caller.
func TestRegistryResolveRefErrorNamesRefNotValue(t *testing.T) {
	r := NewRegistry()
	r.Register(&readOnlyBackend{name: ClusterBackendName, values: map[string][]byte{}})

	_, err := r.ResolveRef(context.Background(), ClusterBackendName, "payments/stripe-key")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.Contains(t, err.Error(), "payments/stripe-key")
}

// A Registry satisfies Resolver so consumers holding local key material can use
// it directly, while a node without key material substitutes an RPC-backed one.
func TestRegistrySatisfiesResolver(t *testing.T) {
	var _ Resolver = NewRegistry()
}

// A backend name has to survive a round trip through a sentinel. The backend
// and the reference share one string separated by the first "/", so a name
// containing one would parse back as a different backend and reference — and if
// both were registered, resolve a different secret.
func TestRegisterRejectsNamesThatCannotRoundTrip(t *testing.T) {
	r := NewRegistry()

	for _, name := range []string{"", "team/a", " padded", "padded "} {
		t.Run(name, func(t *testing.T) {
			err := r.Register(&readOnlyBackend{name: name})
			assert.Error(t, err)

			_, ok := r.Get(name)
			assert.False(t, ok, "a refused backend must not be registered")
		})
	}
}

func TestRegisterAcceptsOrdinaryNames(t *testing.T) {
	r := NewRegistry()

	for _, name := range []string{ClusterBackendName, "prod-vault", "aws_sm", "vault.eu"} {
		require.NoError(t, r.Register(&readOnlyBackend{name: name}), name)
	}
	assert.Len(t, r.Names(), 4)
}
