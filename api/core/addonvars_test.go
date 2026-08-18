package compute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	apiserver "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

type overlayFixture struct {
	ctx   context.Context
	inmem *testutils.InMemEntityServer
	ec    *apiserver.Client
	appID entity.Id
}

func newOverlayFixture(t *testing.T) *overlayFixture {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	ec := inmem.Client

	appID, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)

	return &overlayFixture{ctx: ctx, inmem: inmem, ec: ec, appID: appID}
}

func (f *overlayFixture) version(t *testing.T, name string, vars ...core_v1alpha.ConfigSpecVariables) *core_v1alpha.AppVersion {
	t.Helper()

	cvID, err := f.ec.Create(f.ctx, name+"-cfg", &core_v1alpha.ConfigVersion{
		App:  f.appID,
		Spec: core_v1alpha.ConfigSpec{Variables: vars},
	})
	require.NoError(t, err)

	ver := &core_v1alpha.AppVersion{App: f.appID, Version: name, ConfigVersion: cvID}
	id, err := f.ec.Create(f.ctx, name, ver)
	require.NoError(t, err)
	ver.ID = id
	return ver
}

func (f *overlayFixture) association(t *testing.T, name, status string, vars ...addon_v1alpha.Variables) {
	t.Helper()

	_, err := f.ec.Create(f.ctx, name, &addon_v1alpha.AddonAssociation{
		App:       f.appID,
		Status:    status,
		Variables: vars,
	})
	require.NoError(t, err)
}

func varsByKey(spec *core_v1alpha.ConfigSpec) map[string]core_v1alpha.ConfigSpecVariables {
	out := make(map[string]core_v1alpha.ConfigSpecVariables, len(spec.Variables))
	for _, v := range spec.Variables {
		out[v.Key] = v
	}
	return out
}

// TestResolveRuntimeConfigSuppliesBindingToVersionThatPredatesAddon is the
// MIR-1579 case. The version was built before the addon existed, so it has never
// contained DATABASE_URL; activating it used to strand the app without database
// credentials permanently.
func TestResolveRuntimeConfigSuppliesBindingToVersionThatPredatesAddon(t *testing.T) {
	f := newOverlayFixture(t)

	preAddon := f.version(t, "myapp-v1",
		core_v1alpha.ConfigSpecVariables{Key: "PORT", Value: "3000", Source: SourceConfig})

	f.association(t, "pg-assoc", "active",
		addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://live", Sensitive: true})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, preAddon)
	require.NoError(t, err)

	got := varsByKey(spec)
	require.Contains(t, got, "DATABASE_URL", "the binding must reach a version that predates the addon")
	assert.Equal(t, "postgres://live", got["DATABASE_URL"].Value)
	assert.Equal(t, SourceAddon, got["DATABASE_URL"].Source,
		"deprovision strips only source==addon, so a relabelled var would leak")
	assert.True(t, got["DATABASE_URL"].Sensitive)
	assert.Equal(t, "3000", got["PORT"].Value, "the version's own config must survive")

	// The stored config is untouched: no version has to contain the binding.
	stored, err := ResolveConfig(f.ctx, f.inmem.EAC, preAddon)
	require.NoError(t, err)
	assert.NotContains(t, varsByKey(stored), "DATABASE_URL",
		"ResolveConfig feeds the write paths and must not carry the overlay")
}

// TestResolveRuntimeConfigPrefersAssociationOverStoredCopy covers a version that
// still carries a copy from when provisioning wrote one. The association is the
// record that gets updated, so it wins.
func TestResolveRuntimeConfigPrefersAssociationOverStoredCopy(t *testing.T) {
	f := newOverlayFixture(t)

	ver := f.version(t, "myapp-v2", core_v1alpha.ConfigSpecVariables{
		Key: "DATABASE_URL", Value: "postgres://stored", Sensitive: true, Source: SourceAddon,
	})

	f.association(t, "pg-assoc", "active",
		addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://rotated", Sensitive: true})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	got := varsByKey(spec)
	assert.Equal(t, "postgres://rotated", got["DATABASE_URL"].Value)
	assert.Len(t, spec.Variables, 1, "the binding must replace the copy, not duplicate it")
}

// TestResolveRuntimeConfigDropsCopyFromDestroyedAddon is what dropping buys.
// Deprovision no longer rewrites versions, so a version can outlive the addon
// that contributed to it. Continuing to serve those credentials would point the
// app at a database that no longer exists.
func TestResolveRuntimeConfigDropsCopyFromDestroyedAddon(t *testing.T) {
	f := newOverlayFixture(t)

	ver := f.version(t, "myapp-v1",
		core_v1alpha.ConfigSpecVariables{Key: "PORT", Value: "3000", Source: SourceConfig},
		core_v1alpha.ConfigSpecVariables{
			Key: "DATABASE_URL", Value: "postgres://destroyed", Sensitive: true, Source: SourceAddon,
		})

	// No association at all: the addon was destroyed.
	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	got := varsByKey(spec)
	assert.NotContains(t, got, "DATABASE_URL",
		"a stored copy with no live association behind it must not be served")
	assert.Equal(t, "3000", got["PORT"].Value, "unrelated config must survive")
}

// TestResolveRuntimeConfigNeverOverridesManual pins RFD-59's rule: an addon
// variable never displaces one an operator set.
func TestResolveRuntimeConfigNeverOverridesManual(t *testing.T) {
	f := newOverlayFixture(t)

	ver := f.version(t, "myapp-v1", core_v1alpha.ConfigSpecVariables{
		Key: "DATABASE_URL", Value: "postgres://operator-chose-this", Source: SourceManual,
	})

	f.association(t, "pg-assoc", "active",
		addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://addon"})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	got := varsByKey(spec)
	assert.Equal(t, "postgres://operator-chose-this", got["DATABASE_URL"].Value)
	assert.Equal(t, SourceManual, got["DATABASE_URL"].Source, "the operator's source must not be relabelled")
}

// TestResolveRuntimeConfigOverridesAppTomlDeclaration carries over the rule
// mergeAddonVars used to enforce. Declaring DATABASE_URL in app.toml and then
// attaching a database addon has always meant the addon supplies the real
// value; blocking it would hand the app a placeholder instead of a credential.
func TestResolveRuntimeConfigOverridesAppTomlDeclaration(t *testing.T) {
	f := newOverlayFixture(t)

	ver := f.version(t, "myapp-v1",
		core_v1alpha.ConfigSpecVariables{Key: "PORT", Value: "3000", Source: SourceConfig},
		core_v1alpha.ConfigSpecVariables{
			Key: "DATABASE_URL", Value: "postgres://placeholder", Source: SourceConfig,
			Required: true, Description: "where the database lives",
		})

	f.association(t, "pg-assoc", "active",
		addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://real", Sensitive: true})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	got := varsByKey(spec)
	assert.Equal(t, "postgres://real", got["DATABASE_URL"].Value)
	assert.Equal(t, SourceAddon, got["DATABASE_URL"].Source)
	assert.Len(t, spec.Variables, 2, "the binding must replace the declaration, not sit beside it")

	// The declaration's metadata is what `miren env list` renders and what the
	// next build validates against, so shadowing must not blank it.
	assert.True(t, got["DATABASE_URL"].Required, "app.toml's required flag must survive being shadowed")
	assert.Equal(t, "where the database lives", got["DATABASE_URL"].Description)
}

// TestResolveRuntimeConfigTreatsEmptySourceAsOperatorSet covers versions written
// before the source field existed. mergeVariablesFromAppConfig has always read
// an empty source as operator-set, and an addon must not displace those either.
func TestResolveRuntimeConfigTreatsEmptySourceAsOperatorSet(t *testing.T) {
	f := newOverlayFixture(t)

	ver := f.version(t, "myapp-v1",
		core_v1alpha.ConfigSpecVariables{Key: "DATABASE_URL", Value: "postgres://legacy"})

	f.association(t, "pg-assoc", "active",
		addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://addon"})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	got := varsByKey(spec)
	assert.Equal(t, "postgres://legacy", got["DATABASE_URL"].Value,
		"an unsourced variable predates the field and is treated as operator-set")
}

// TestResolveRuntimeConfigIgnoresInactiveAssociations covers teardown and
// mid-provision. Supplying credentials from an association being deprovisioned
// would keep an app pointed at a database about to disappear.
func TestResolveRuntimeConfigIgnoresInactiveAssociations(t *testing.T) {
	for _, status := range []string{"pending", "provisioning", "deprovisioning", "error"} {
		t.Run(status, func(t *testing.T) {
			f := newOverlayFixture(t)
			ver := f.version(t, "myapp-v1")
			f.association(t, "pg-assoc", status,
				addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://x"})

			spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
			require.NoError(t, err)
			assert.NotContains(t, varsByKey(spec), "DATABASE_URL",
				"only an active association contributes bindings")
		})
	}
}

// TestResolveRuntimeConfigWithoutAddons is the overwhelmingly common path: an app
// with no addons must resolve exactly as ResolveConfig does, in the same order.
func TestResolveRuntimeConfigWithoutAddons(t *testing.T) {
	f := newOverlayFixture(t)
	ver := f.version(t, "myapp-v1",
		core_v1alpha.ConfigSpecVariables{Key: "PORT", Value: "3000", Source: SourceConfig},
		core_v1alpha.ConfigSpecVariables{Key: "LOG_LEVEL", Value: "info", Source: SourceManual})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	stored, err := ResolveConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	assert.Equal(t, stored.Variables, spec.Variables)
}

// TestResolveRuntimeConfigMergesMultipleAddons covers an app holding more than
// one addon, the composition case MIR-1458 established has to work.
func TestResolveRuntimeConfigMergesMultipleAddons(t *testing.T) {
	f := newOverlayFixture(t)
	ver := f.version(t, "myapp-v1")

	f.association(t, "pg-assoc", "active",
		addon_v1alpha.Variables{Key: "DATABASE_URL", Value: "postgres://x"})
	f.association(t, "mc-assoc", "active",
		addon_v1alpha.Variables{Key: "MEMCACHE_URL", Value: "memcache://y"})

	spec, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)

	got := varsByKey(spec)
	assert.Equal(t, "postgres://x", got["DATABASE_URL"].Value)
	assert.Equal(t, "memcache://y", got["MEMCACHE_URL"].Value)
}

// TestResolveRuntimeConfigIsDeterministicAcrossAssociations pins which addon
// wins when two supply the same key. AdjustEnvVars normally makes the second one
// rename at provision time, so this is unusual, but the resolved config must be
// a function of stored state rather than of the order the entity store returned.
func TestResolveRuntimeConfigIsDeterministicAcrossAssociations(t *testing.T) {
	f := newOverlayFixture(t)
	ver := f.version(t, "myapp-v1")

	f.association(t, "assoc-a", "active",
		addon_v1alpha.Variables{Key: "SHARED_URL", Value: "from-a"})
	f.association(t, "assoc-b", "active",
		addon_v1alpha.Variables{Key: "SHARED_URL", Value: "from-b"})

	first, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
	require.NoError(t, err)
	winner := varsByKey(first)["SHARED_URL"].Value
	require.NotEmpty(t, winner)

	for i := 0; i < 8; i++ {
		again, err := ResolveRuntimeConfig(f.ctx, f.inmem.EAC, ver)
		require.NoError(t, err)
		assert.Equal(t, winner, varsByKey(again)["SHARED_URL"].Value,
			"the same stored state must always resolve to the same value")
	}
}
