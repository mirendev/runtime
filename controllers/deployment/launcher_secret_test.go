package deployment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/secret"
)

// deployWithSecretRef stands up an app whose config sources a variable from a
// secret backend, reconciles it, and returns the pool that resulted.
func deployWithSecretRef(t *testing.T, pinnedRef string) compute_v1alpha.SandboxPool {
	t.Helper()

	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	configVer := &core_v1alpha.ConfigVersion{
		App: app.ID,
		Spec: core_v1alpha.ConfigSpec{
			Variables: []core_v1alpha.ConfigSpecVariables{
				{Key: "DATABASE_POOL", Value: "10", Source: "manual"},
				{
					Key:       "STRIPE_API_KEY",
					Value:     pinnedRef,
					Backend:   secret.ClusterBackendName,
					Source:    "manual",
					Sensitive: true,
				},
			},
			Services: []core_v1alpha.ConfigSpecServices{{
				Name:        "web",
				Concurrency: core_v1alpha.ConfigSpecServicesConcurrency{Mode: "fixed", NumInstances: 1},
			}},
		},
	}
	cvID, err := server.Client.Create(ctx, "test-cfg", configVer)
	require.NoError(t, err)

	version := &core_v1alpha.AppVersion{
		App:           app.ID,
		Version:       "v1",
		ImageUrl:      "test:latest",
		ConfigVersion: cvID,
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	require.NoError(t, server.Client.Update(ctx, app))

	launcher := newTestLauncher(log, server.EAC)
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)
	return pools[0]
}

// The sandbox spec is persisted, so a resolved value written into it would sit
// in the entity store in plaintext — the exact thing referencing a secret
// instead of inlining it is meant to prevent. The spec must carry only the
// reference.
func TestLauncherWritesSecretReferencesNotValues(t *testing.T) {
	pool := deployWithSecretRef(t, "payments/stripe-key@x1A")

	var stripeEntry string
	for _, entry := range pool.SandboxSpec.Container[0].Env {
		if strings.HasPrefix(entry, "STRIPE_API_KEY=") {
			stripeEntry = entry
		}
	}
	require.NotEmpty(t, stripeEntry, "the referenced variable should still reach the spec")

	value := strings.TrimPrefix(stripeEntry, "STRIPE_API_KEY=")
	assert.True(t, secret.IsSentinel(value), "expected a reference, got %q", value)

	backend, ref, ok := secret.ParseSentinel(value)
	require.True(t, ok)
	assert.Equal(t, secret.ClusterBackendName, backend)

	// The pinned version rides along, so what the sandbox resolves is exactly
	// what this ConfigVersion froze.
	assert.Equal(t, "payments/stripe-key@x1A", ref)
}

// Belt and braces: nothing anywhere in the persisted pool may contain the
// secret's value, however the spec is serialized.
func TestPersistedPoolContainsNoSecretValue(t *testing.T) {
	pool := deployWithSecretRef(t, "payments/stripe-key@x1A")

	encoded, err := json.Marshal(pool)
	require.NoError(t, err)

	// The value that would have been resolved had materialization happened here.
	assert.NotContains(t, string(encoded), "sk_live")
	assert.Contains(t, string(encoded), secret.SentinelScheme)
}

// An ordinary inline variable must be entirely unaffected, so a cluster using
// no secrets sees no change.
func TestLauncherLeavesInlineVariablesAlone(t *testing.T) {
	pool := deployWithSecretRef(t, "payments/stripe-key@x1A")

	assert.Contains(t, pool.SandboxSpec.Container[0].Env, "DATABASE_POOL=10")
}
