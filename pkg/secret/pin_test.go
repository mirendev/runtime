package secret

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedResolver resolves a floating reference to a named current version and
// honours an explicit pin, which is the behaviour every backend guarantees.
type fixedResolver struct {
	current map[string]string // path -> current version
	err     error
	calls   int
}

func (r *fixedResolver) ResolveRef(ctx context.Context, backend, ref string) (SecretValue, error) {
	r.calls++
	if r.err != nil {
		return SecretValue{}, r.err
	}

	path, version, err := ParseRef(ref)
	if err != nil {
		return SecretValue{}, err
	}
	if version == "" {
		var ok bool
		version, ok = r.current[path]
		if !ok {
			return SecretValue{}, ErrNotFound
		}
	}

	return SecretValue{Ref: FormatRef(path, version), Bytes: []byte("value-of-" + path)}, nil
}

func TestPinResolvesFloatingReferences(t *testing.T) {
	r := &fixedResolver{current: map[string]string{"payments/stripe-key": "x1A"}}

	pinned, err := Pin(context.Background(), r, []Reference{
		{Backend: ClusterBackendName, Ref: "payments/stripe-key", Key: "STRIPE_API_KEY"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"payments/stripe-key@x1A"}, pinned)
}

// Pinning an already-pinned reference must return it unchanged. That identity is
// what lets a hand-set reference stay put while an app.toml reference re-pins on
// every deploy, with no second piece of state to keep in sync.
func TestPinIsIdempotent(t *testing.T) {
	r := &fixedResolver{current: map[string]string{"payments/stripe-key": "m4Q"}}

	refs := []Reference{{Backend: ClusterBackendName, Ref: "payments/stripe-key@x1A", Key: "STRIPE_API_KEY"}}

	first, err := Pin(context.Background(), r, refs)
	require.NoError(t, err)
	assert.Equal(t, []string{"payments/stripe-key@x1A"}, first)

	second, err := Pin(context.Background(), r, []Reference{{Backend: ClusterBackendName, Ref: first[0], Key: "STRIPE_API_KEY"}})
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestPinPreservesOrder(t *testing.T) {
	r := &fixedResolver{current: map[string]string{
		"a": "1", "b": "2", "c": "3",
	}}

	pinned, err := Pin(context.Background(), r, []Reference{
		{Backend: ClusterBackendName, Ref: "a", Key: "A"},
		{Backend: ClusterBackendName, Ref: "b", Key: "B"},
		{Backend: ClusterBackendName, Ref: "c", Key: "C"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a@1", "b@2", "c@3"}, pinned)
}

// A failure has to say which variable could not be resolved, since that is the
// only thing the operator can act on — and must not quote the value.
func TestPinErrorNamesTheVariable(t *testing.T) {
	r := &fixedResolver{current: map[string]string{}}

	_, err := Pin(context.Background(), r, []Reference{
		{Backend: ClusterBackendName, Ref: "payments/stripe-key", Key: "STRIPE_API_KEY"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STRIPE_API_KEY")
	assert.Contains(t, err.Error(), ClusterBackendName)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPinErrorNamesTheService(t *testing.T) {
	r := &fixedResolver{current: map[string]string{}}

	_, err := Pin(context.Background(), r, []Reference{
		{Backend: ClusterBackendName, Ref: "payments/stripe-key", Key: "STRIPE_API_KEY", Service: "worker"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker")
}

func TestPinRejectsIncompleteReferences(t *testing.T) {
	r := &fixedResolver{current: map[string]string{"a": "1"}}

	t.Run("no backend", func(t *testing.T) {
		_, err := Pin(context.Background(), r, []Reference{{Ref: "a", Key: "A"}})
		assert.Error(t, err)
	})

	t.Run("no secret", func(t *testing.T) {
		_, err := Pin(context.Background(), r, []Reference{{Backend: ClusterBackendName, Key: "A"}})
		assert.Error(t, err)
	})
}

func TestPinWithNoReferencesNeverCallsTheResolver(t *testing.T) {
	r := &fixedResolver{current: map[string]string{}}

	pinned, err := Pin(context.Background(), r, nil)
	require.NoError(t, err)
	assert.Empty(t, pinned)
	assert.Zero(t, r.calls)
}
