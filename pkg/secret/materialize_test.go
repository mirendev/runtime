package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type envResolver struct {
	values map[string][]byte
	err    error
	calls  int
}

func (r *envResolver) ResolveRef(ctx context.Context, backend, ref string) (SecretValue, error) {
	r.calls++
	if r.err != nil {
		return SecretValue{}, r.err
	}
	val, ok := r.values[backend+"/"+ref]
	if !ok {
		return SecretValue{}, ErrNotFound
	}
	return SecretValue{Ref: ref, Bytes: val}, nil
}

func TestFormatAndParseSentinelRoundTrip(t *testing.T) {
	s := FormatSentinel(ClusterBackendName, "payments/stripe-key@x1A")
	assert.Equal(t, "miren+secret://cluster/payments/stripe-key@x1A", s)

	backend, ref, ok := ParseSentinel(s)
	require.True(t, ok)
	assert.Equal(t, ClusterBackendName, backend)
	assert.Equal(t, "payments/stripe-key@x1A", ref)
}

func TestParseSentinelIgnoresOrdinaryValues(t *testing.T) {
	for _, value := range []string{
		"",
		"postgres://localhost/db",
		"just a value",
		"miren+secret://",
		"miren+secret://cluster",
		"miren+secret:///ref",
	} {
		t.Run(value, func(t *testing.T) {
			_, _, ok := ParseSentinel(value)
			assert.False(t, ok)
		})
	}
}

func TestMaterializeEnvSubstitutesReferences(t *testing.T) {
	r := &envResolver{values: map[string][]byte{
		"cluster/payments/stripe-key@x1A": []byte("sk_live_abc123"),
	}}

	env := []string{
		"PORT=3000",
		"STRIPE_API_KEY=" + FormatSentinel(ClusterBackendName, "payments/stripe-key@x1A"),
	}

	out, err := MaterializeEnv(context.Background(), r, env)
	require.NoError(t, err)
	assert.Equal(t, []string{"PORT=3000", "STRIPE_API_KEY=sk_live_abc123"}, out)
}

// The input is what came off a persisted spec, so materializing must not write
// a value back into it.
func TestMaterializeEnvLeavesTheInputUntouched(t *testing.T) {
	r := &envResolver{values: map[string][]byte{
		"cluster/payments/stripe-key@x1A": []byte("sk_live_abc123"),
	}}

	sentinel := FormatSentinel(ClusterBackendName, "payments/stripe-key@x1A")
	env := []string{"STRIPE_API_KEY=" + sentinel}

	_, err := MaterializeEnv(context.Background(), r, env)
	require.NoError(t, err)
	assert.Equal(t, []string{"STRIPE_API_KEY=" + sentinel}, env)
}

// The overwhelmingly common case is a sandbox with no secrets at all, which
// must neither allocate nor require a resolver.
func TestMaterializeEnvWithNoReferencesIsAPassThrough(t *testing.T) {
	env := []string{"PORT=3000", "DATABASE_POOL=10"}

	out, err := MaterializeEnv(context.Background(), nil, env)
	require.NoError(t, err)
	assert.Equal(t, env, out)
}

// A value that happens to contain "=" must survive intact — a URL with a query
// string is an ordinary env var, not something to re-split.
func TestMaterializeEnvPreservesValuesContainingEquals(t *testing.T) {
	r := &envResolver{values: map[string][]byte{
		"cluster/db/url": []byte("postgres://u:p@h/db?sslmode=require"),
	}}

	env := []string{
		"OTHER=a=b=c",
		"DATABASE_URL=" + FormatSentinel(ClusterBackendName, "db/url"),
	}

	out, err := MaterializeEnv(context.Background(), r, env)
	require.NoError(t, err)
	assert.Equal(t, "OTHER=a=b=c", out[0])
	assert.Equal(t, "DATABASE_URL=postgres://u:p@h/db?sslmode=require", out[1])
}

// Starting a container with the reference still in place would hand the app a
// URL where its credential belongs, and it would fail much later somewhere
// harder to read. Every way this can go wrong must fail here instead.
func TestMaterializeEnvFailsClosed(t *testing.T) {
	sentinel := FormatSentinel(ClusterBackendName, "payments/stripe-key@x1A")

	t.Run("no resolver available", func(t *testing.T) {
		_, err := MaterializeEnv(context.Background(), nil, []string{"STRIPE_API_KEY=" + sentinel})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "STRIPE_API_KEY")
	})

	t.Run("secret not found", func(t *testing.T) {
		r := &envResolver{values: map[string][]byte{}}
		_, err := MaterializeEnv(context.Background(), r, []string{"STRIPE_API_KEY=" + sentinel})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Contains(t, err.Error(), "STRIPE_API_KEY")
	})

	t.Run("version revoked", func(t *testing.T) {
		r := &envResolver{err: ErrVersionNotEnabled}
		_, err := MaterializeEnv(context.Background(), r, []string{"STRIPE_API_KEY=" + sentinel})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrVersionNotEnabled)
	})

	t.Run("malformed reference", func(t *testing.T) {
		r := &envResolver{values: map[string][]byte{}}
		_, err := MaterializeEnv(context.Background(), r, []string{"STRIPE_API_KEY=" + SentinelScheme + "cluster"})
		require.Error(t, err)

		var malformed MalformedSentinelError
		require.True(t, errors.As(err, &malformed))
		assert.Equal(t, "STRIPE_API_KEY", malformed.Key)
	})
}

// An error may name the variable an operator has to fix, but never the value it
// was trying to fetch.
func TestMaterializeEnvErrorNeverCarriesTheValue(t *testing.T) {
	const value = "sk_live_abc123"
	r := &envResolver{
		values: map[string][]byte{"cluster/payments/stripe-key@x1A": []byte(value)},
		err:    ErrVersionNotEnabled,
	}

	_, err := MaterializeEnv(context.Background(), r,
		[]string{"STRIPE_API_KEY=" + FormatSentinel(ClusterBackendName, "payments/stripe-key@x1A")})
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), value))
}
