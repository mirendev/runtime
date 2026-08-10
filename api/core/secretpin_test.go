package compute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/secret"
)

// stubResolver resolves a floating reference to a fixed current version and
// honours an explicit pin, as every backend guarantees.
type stubResolver struct {
	current map[string]string
	calls   int
}

func (r *stubResolver) ResolveRef(ctx context.Context, backend, ref string) (secret.SecretValue, error) {
	r.calls++

	path, version, err := secret.ParseRef(ref)
	if err != nil {
		return secret.SecretValue{}, err
	}
	if version == "" {
		var ok bool
		version, ok = r.current[path]
		if !ok {
			return secret.SecretValue{}, secret.ErrNotFound
		}
	}
	return secret.SecretValue{Ref: secret.FormatRef(path, version), Bytes: []byte("value")}, nil
}

func TestSecretReferencesFindsSharedAndPerServiceVariables(t *testing.T) {
	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "PLAIN", Value: "10"},
			{Key: "STRIPE_API_KEY", Value: "payments/stripe-key", Backend: "cluster"},
		},
		Services: []core_v1alpha.ConfigSpecServices{
			{
				Name: "worker",
				Env: []core_v1alpha.ConfigSpecServicesEnv{
					{Key: "QUEUE", Value: "default"},
					{Key: "SIGNING_KEY", Value: "auth/signing", Backend: "prod-vault"},
				},
			},
		},
	}

	refs := SecretReferences(spec)
	require.Len(t, refs, 2)

	assert.Equal(t, secret.Reference{Backend: "cluster", Ref: "payments/stripe-key", Key: "STRIPE_API_KEY"}, refs[0])
	assert.Equal(t, secret.Reference{Backend: "prod-vault", Ref: "auth/signing", Key: "SIGNING_KEY", Service: "worker"}, refs[1])
}

func TestPinSecretsRewritesReferencesInPlace(t *testing.T) {
	r := &stubResolver{current: map[string]string{
		"payments/stripe-key": "x1A",
		"auth/signing":        "42",
	}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "PLAIN", Value: "10"},
			{Key: "STRIPE_API_KEY", Value: "payments/stripe-key", Backend: "cluster"},
		},
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "worker",
			Env: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "QUEUE", Value: "default"},
				{Key: "SIGNING_KEY", Value: "auth/signing", Backend: "prod-vault"},
			},
		}},
	}

	require.NoError(t, PinSecrets(context.Background(), r, spec))

	assert.Equal(t, "payments/stripe-key@x1A", spec.Variables[1].Value)
	assert.Equal(t, "auth/signing@42", spec.Services[0].Env[1].Value)

	// Inline literals are left exactly as they were.
	assert.Equal(t, "10", spec.Variables[0].Value)
	assert.Equal(t, "default", spec.Services[0].Env[0].Value)
}

// A redeploy re-pins a floating reference to whatever is current — that is how a
// rotation reaches an app — while a reference authored with an explicit version
// stays where it was put.
func TestPinSecretsRepinsFloatingButHoldsExplicitVersions(t *testing.T) {
	r := &stubResolver{current: map[string]string{"payments/stripe-key": "m4Q"}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "STRIPE_API_KEY", Value: "payments/stripe-key", Backend: "cluster"},
			{Key: "STRIPE_API_KEY_PREV", Value: "payments/stripe-key@x1A", Backend: "cluster"},
		},
	}

	require.NoError(t, PinSecrets(context.Background(), r, spec))

	assert.Equal(t, "payments/stripe-key@m4Q", spec.Variables[0].Value)
	assert.Equal(t, "payments/stripe-key@x1A", spec.Variables[1].Value)
}

// Pinning the output of a previous pin must be a no-op, which is what keeps an
// already-pinned ConfigVersion stable across the mint paths that re-run it.
func TestPinSecretsIsIdempotent(t *testing.T) {
	r := &stubResolver{current: map[string]string{"payments/stripe-key": "x1A"}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "STRIPE_API_KEY", Value: "payments/stripe-key", Backend: "cluster"},
		},
	}

	require.NoError(t, PinSecrets(context.Background(), r, spec))
	first := spec.Variables[0].Value

	// The secret rotates underneath, but the now-pinned reference does not move.
	r.current["payments/stripe-key"] = "m4Q"
	require.NoError(t, PinSecrets(context.Background(), r, spec))

	assert.Equal(t, first, spec.Variables[0].Value)
}

// A config that references nothing must not depend on a resolver at all, so a
// cluster with no secret backends keeps working exactly as before.
func TestPinSecretsSkipsConfigsWithNoReferences(t *testing.T) {
	r := &stubResolver{current: map[string]string{}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{{Key: "PLAIN", Value: "10"}},
	}

	require.NoError(t, PinSecrets(context.Background(), r, spec))
	assert.Zero(t, r.calls)
}

// A resolution failure must fail the mint rather than leaving the variable at
// its unresolved reference, which would deploy an app with a literal path where
// its credential should be.
func TestPinSecretsFailsClosed(t *testing.T) {
	r := &stubResolver{current: map[string]string{}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "STRIPE_API_KEY", Value: "payments/stripe-key", Backend: "cluster"},
		},
	}

	err := PinSecrets(context.Background(), r, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STRIPE_API_KEY")
	assert.Equal(t, "payments/stripe-key", spec.Variables[0].Value)
}

// Backend identity travels in-band, so the mapping is breakable from the other
// side too: a literal whose value starts with the scheme would be materialized
// as though it named a secret. Refuse it where every config passes through.
func TestPinSecretsRejectsALiteralPosingAsAReference(t *testing.T) {
	r := &stubResolver{current: map[string]string{}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "SNEAKY", Value: secret.FormatSentinel("cluster", "payments/stripe-key@x1A")},
		},
	}

	err := PinSecrets(context.Background(), r, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SNEAKY")
	assert.Zero(t, r.calls, "it should be refused before any resolution")
}

func TestPinSecretsRejectsALiteralPosingAsAReferenceInAService(t *testing.T) {
	r := &stubResolver{current: map[string]string{}}

	spec := &core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{
			Name: "worker",
			Env: []core_v1alpha.ConfigSpecServicesEnv{
				{Key: "SNEAKY", Value: secret.FormatSentinel("cluster", "payments/stripe-key@x1A")},
			},
		}},
	}

	err := PinSecrets(context.Background(), r, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker")
}

// A genuine reference carries the same value shape but names its backend, so it
// must still pin normally.
func TestPinSecretsAllowsARealReference(t *testing.T) {
	r := &stubResolver{current: map[string]string{"payments/stripe-key": "x1A"}}

	spec := &core_v1alpha.ConfigSpec{
		Variables: []core_v1alpha.ConfigSpecVariables{
			{Key: "STRIPE_API_KEY", Value: "payments/stripe-key", Backend: "cluster"},
		},
	}

	require.NoError(t, PinSecrets(context.Background(), r, spec))
	assert.Equal(t, "payments/stripe-key@x1A", spec.Variables[0].Value)
}
