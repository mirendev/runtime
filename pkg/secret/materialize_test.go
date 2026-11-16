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
	owners map[string]string // backend/path -> owning app id
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
	path, _, _ := ParseRef(ref)
	return SecretValue{Ref: ref, Bytes: val, Owner: r.owners[backend+"/"+path]}, nil
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

	out, err := MaterializeEnv(context.Background(), r, env, nil)
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

	_, err := MaterializeEnv(context.Background(), r, env, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"STRIPE_API_KEY=" + sentinel}, env)
}

// The overwhelmingly common case is a sandbox with no secrets at all, which
// must neither allocate nor require a resolver.
func TestMaterializeEnvWithNoReferencesIsAPassThrough(t *testing.T) {
	env := []string{"PORT=3000", "DATABASE_POOL=10"}

	out, err := MaterializeEnv(context.Background(), nil, env, nil)
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

	out, err := MaterializeEnv(context.Background(), r, env, nil)
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
		_, err := MaterializeEnv(context.Background(), nil, []string{"STRIPE_API_KEY=" + sentinel}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "STRIPE_API_KEY")
	})

	t.Run("secret not found", func(t *testing.T) {
		r := &envResolver{values: map[string][]byte{}}
		_, err := MaterializeEnv(context.Background(), r, []string{"STRIPE_API_KEY=" + sentinel}, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Contains(t, err.Error(), "STRIPE_API_KEY")
	})

	t.Run("version revoked", func(t *testing.T) {
		r := &envResolver{err: ErrVersionNotEnabled}
		_, err := MaterializeEnv(context.Background(), r, []string{"STRIPE_API_KEY=" + sentinel}, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrVersionNotEnabled)
	})

	t.Run("malformed reference", func(t *testing.T) {
		r := &envResolver{values: map[string][]byte{}}
		_, err := MaterializeEnv(context.Background(), r, []string{"STRIPE_API_KEY=" + SentinelScheme + "cluster"}, nil)
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
		[]string{"STRIPE_API_KEY=" + FormatSentinel(ClusterBackendName, "payments/stripe-key@x1A")}, nil)
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), value))
}

// The runner hands an owned secret only to its own app's sandboxes, however the
// reference reached the spec, and asks which app the sandbox is at most once.
func TestMaterializeEnvRefusesAnotherAppsSecret(t *testing.T) {
	r := &envResolver{
		values: map[string][]byte{
			"cluster/shop/db@x1A":     []byte("shop-pw"),
			"cluster/other/db@x2B":    []byte("other-pw"),
			"cluster/shared/token@x3": []byte("shared"),
		},
		owners: map[string]string{
			"cluster/shop/db":  "app/shop",
			"cluster/other/db": "app/other",
		},
	}
	lookups := 0
	shop := func(context.Context) (string, error) {
		lookups++
		return "app/shop", nil
	}

	out, err := MaterializeEnv(context.Background(), r, []string{
		"DB=" + FormatSentinel(ClusterBackendName, "shop/db@x1A"),
		"TOKEN=" + FormatSentinel(ClusterBackendName, "shared/token@x3"),
	}, shop)
	require.NoError(t, err)
	assert.Equal(t, []string{"DB=shop-pw", "TOKEN=shared"}, out)
	assert.Equal(t, 1, lookups)

	_, err = MaterializeEnv(context.Background(), r, []string{
		"DB=" + FormatSentinel(ClusterBackendName, "other/db@x2B"),
	}, shop)
	require.ErrorIs(t, err, ErrOwnedByOtherApp)

	// A sandbox that belongs to no app gets only unowned secrets.
	_, err = MaterializeEnv(context.Background(), r, []string{
		"DB=" + FormatSentinel(ClusterBackendName, "shop/db@x1A"),
	}, nil)
	require.ErrorIs(t, err, ErrOwnedByOtherApp)
}

type appTellingResolver struct {
	envResolver
	apps []string
}

func (r *appTellingResolver) ResolveRefForApp(ctx context.Context, backend, ref, app string) (SecretValue, error) {
	r.apps = append(r.apps, app)
	return r.ResolveRef(ctx, backend, ref)
}

// A resolver that can be told the sandbox's app is told it on every resolve, so
// a remote coordinator can refuse an owned secret before sending it.
func TestMaterializeEnvTellsAnAppResolverTheApp(t *testing.T) {
	r := &appTellingResolver{envResolver: envResolver{values: map[string][]byte{
		"cluster/shared/a@x1": []byte("a"),
		"cluster/shared/b@x2": []byte("b"),
	}}}
	lookups := 0
	shop := func(context.Context) (string, error) {
		lookups++
		return "app/shop", nil
	}

	_, err := MaterializeEnv(context.Background(), r, []string{
		"A=" + FormatSentinel(ClusterBackendName, "shared/a@x1"),
		"B=" + FormatSentinel(ClusterBackendName, "shared/b@x2"),
	}, shop)
	require.NoError(t, err)
	assert.Equal(t, []string{"app/shop", "app/shop"}, r.apps)
	assert.Equal(t, 1, lookups)
}
