package addon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func setupAssocTest(t *testing.T) (context.Context, *entityserver.Client, *testutils.InMemEntityServer) {
	t.Helper()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	return context.Background(), entityserver.NewClient(slog.Default(), inmem.EAC), inmem
}

// appOnVersion creates an app whose active version carries vars.
func appOnVersion(t *testing.T, ctx context.Context, ec *entityserver.Client, name string,
	vars ...core_v1alpha.ConfigSpecVariables) entity.Id {
	t.Helper()

	appID, err := ec.Create(ctx, name, &core_v1alpha.App{})
	require.NoError(t, err)

	cvID, err := ec.Create(ctx, name+"-cfg", &core_v1alpha.ConfigVersion{
		App:  appID,
		Spec: core_v1alpha.ConfigSpec{Variables: vars},
	})
	require.NoError(t, err)

	verID, err := ec.Create(ctx, name+"-v1", &core_v1alpha.AppVersion{
		App: appID, Version: name + "-v1", ConfigVersion: cvID,
	})
	require.NoError(t, err)

	require.NoError(t, ec.Patch(ctx, appID, 0, entity.Ref(core_v1alpha.AppActiveVersionId, verID)))
	return appID
}

func assocVars(t *testing.T, ctx context.Context, ec *entityserver.Client, id entity.Id) map[string]addon_v1alpha.Variables {
	t.Helper()
	var got addon_v1alpha.AddonAssociation
	require.NoError(t, ec.GetById(ctx, id, &got))
	out := make(map[string]addon_v1alpha.Variables, len(got.Variables))
	for _, v := range got.Variables {
		out[v.Key] = v
	}
	return out
}

// TestSetAssociationVarsReplacesRatherThanAppends is the regression test for the
// trap that kept association.variables frozen in the first place. Patch and
// Update both MERGE multi-valued attributes, so the obvious implementation
// leaves the superseded credential sitting alongside the new one and a reader
// cannot tell which is live. Only Replace can overwrite a many-valued attribute.
//
// It also pins the other half of using Replace: because it rewrites the whole
// entity, every attribute this function does not manage has to survive.
func TestSetAssociationVarsReplacesRatherThanAppends(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	appID := appOnVersion(t, ctx, ec, "myapp")
	addonID, err := ec.Create(ctx, "some-addon", &addon_v1alpha.Addon{Name: "miren-postgresql"})
	require.NoError(t, err)

	assocID, err := ec.Create(ctx, "rotate-assoc", &addon_v1alpha.AddonAssociation{
		App:     appID,
		Addon:   addonID,
		Variant: "small",
		Status:  "active",
		Variables: []addon_v1alpha.Variables{
			{Key: "PGPASSWORD", Value: "old", Sensitive: true},
			{Key: "DATABASE_URL", Value: "postgres://old", Sensitive: true},
		},
	})
	require.NoError(t, err)

	require.NoError(t, setAssociationVars(ctx, inmem.EAC, assocID, []addon.Variable{
		{Key: "PGPASSWORD", Value: "new", Sensitive: true},
		{Key: "DATABASE_URL", Value: "postgres://new", Sensitive: true},
	}))

	got := assocVars(t, ctx, ec, assocID)
	require.Len(t, got, 2, "a merging write would have left the old pair alongside the new one")
	assert.Equal(t, "new", got["PGPASSWORD"].Value)
	assert.Equal(t, "postgres://new", got["DATABASE_URL"].Value)
	assert.True(t, got["PGPASSWORD"].Sensitive, "sensitivity must carry over")

	var full addon_v1alpha.AddonAssociation
	require.NoError(t, ec.GetById(ctx, assocID, &full))
	assert.Equal(t, appID, full.App, "the app ref must survive a full-entity replace")
	assert.Equal(t, addonID, full.Addon, "the addon ref must survive a full-entity replace")
	assert.Equal(t, "small", full.Variant, "unmanaged attrs must survive a full-entity replace")
	assert.Equal(t, "active", full.Status, "status must survive a full-entity replace")
}

// TestSetAssociationVarsIsIdempotent keeps a retry from rewriting the entity on
// every pass. Providers are idempotent for a fixed secret, so a retry arrives
// with the values already recorded.
func TestSetAssociationVarsIsIdempotent(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	assocID, err := ec.Create(ctx, "rotate-assoc", &addon_v1alpha.AddonAssociation{
		Status:    "active",
		Variables: []addon_v1alpha.Variables{{Key: "PGPASSWORD", Value: "same", Sensitive: true}},
	})
	require.NoError(t, err)

	vars := []addon.Variable{{Key: "PGPASSWORD", Value: "same", Sensitive: true}}
	require.NoError(t, setAssociationVars(ctx, inmem.EAC, assocID, vars))

	resp, err := inmem.EAC.Get(ctx, assocID.String())
	require.NoError(t, err)
	revAfter := resp.Entity().Revision()

	require.NoError(t, setAssociationVars(ctx, inmem.EAC, assocID, vars))

	resp, err = inmem.EAC.Get(ctx, assocID.String())
	require.NoError(t, err)
	assert.Equal(t, revAfter, resp.Entity().Revision(),
		"re-recording identical variables must not write the entity")
}

// TestSetAssociationVarsCollapsesDuplicateKeys covers a set arriving with the
// same key twice. A container environment is a map, so only one value can win,
// and storing both would leave a reader unable to tell which. It also protects
// the equality check: with duplicates, two different sets can share a length and
// a key-to-value map, and the write would be skipped as a no-op.
func TestSetAssociationVarsCollapsesDuplicateKeys(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	assocID, err := ec.Create(ctx, "pg-assoc", &addon_v1alpha.AddonAssociation{Status: "active"})
	require.NoError(t, err)

	require.NoError(t, setAssociationVars(ctx, inmem.EAC, assocID, []addon.Variable{
		{Key: "PGPASSWORD", Value: "first"},
		{Key: "PGPASSWORD", Value: "second"},
	}))

	got := assocVars(t, ctx, ec, assocID)
	require.Len(t, got, 1, "a repeated key must be stored once")
	assert.Equal(t, "second", got["PGPASSWORD"].Value, "the last value wins")
}

// TestSetAssociationVarsDoesNotSkipWhenIncomingHasDuplicates is the regression
// test for the skipped write.
//
// The equality check builds its map from the stored set and looks up the
// incoming one, so a repeated incoming key collapses. {A=1, B=2} stored against
// {A=1, A=1} incoming then matches on length and on every lookup, and the write
// is skipped as a no-op. The association keeps B, which the addon no longer
// supplies, and the app keeps resolving it.
func TestSetAssociationVarsDoesNotSkipWhenIncomingHasDuplicates(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	assocID, err := ec.Create(ctx, "pg-assoc", &addon_v1alpha.AddonAssociation{
		Status: "active",
		Variables: []addon_v1alpha.Variables{
			{Key: "PGPASSWORD", Value: "one"},
			{Key: "PGHOST", Value: "two"},
		},
	})
	require.NoError(t, err)

	require.NoError(t, setAssociationVars(ctx, inmem.EAC, assocID, []addon.Variable{
		{Key: "PGPASSWORD", Value: "one"},
		{Key: "PGPASSWORD", Value: "one"},
	}))

	got := assocVars(t, ctx, ec, assocID)
	assert.NotContains(t, got, "PGHOST",
		"a variable the addon no longer supplies must not survive because the comparison collapsed a duplicate")
	require.Len(t, got, 1)
	assert.Equal(t, "one", got["PGPASSWORD"].Value)
}

// TestReportStaleAssociationVariablesFlagsDisagreement covers the state a
// rotation on v0.12.x leaves behind: the version stores the rotated value and
// the association still holds the provision-time one. It is reported, never
// repaired, because repairing it from a boot path means writing to a live
// credential with no reliable way to tell which record is fresher.
func TestReportStaleAssociationVariablesFlagsDisagreement(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	appID := appOnVersion(t, ctx, ec, "myapp",
		core_v1alpha.ConfigSpecVariables{
			Key: "DATABASE_URL", Value: "postgres://rotated", Sensitive: true, Source: coreutil.SourceAddon,
		})

	assocID, err := ec.Create(ctx, "pg-assoc", &addon_v1alpha.AddonAssociation{
		App:       appID,
		Status:    "active",
		Variables: []addon_v1alpha.Variables{{Key: "DATABASE_URL", Value: "postgres://provisioned", Sensitive: true}},
	})
	require.NoError(t, err)

	stale, checked, err := ReportStaleAssociationVariables(ctx, slog.Default(), ec, inmem.EAC)
	require.NoError(t, err)
	assert.Equal(t, 1, stale)
	assert.Equal(t, 1, checked)

	// Nothing was written. The association keeps what it had, and the app keeps
	// running on it, which is the whole point of reporting rather than repairing.
	got := assocVars(t, ctx, ec, assocID)
	assert.Equal(t, "postgres://provisioned", got["DATABASE_URL"].Value,
		"the check must not write to a credential")
}

// TestReportStaleAssociationVariablesQuietWhenAgreed is the overwhelmingly
// common case: an app that never rotated, where both records match.
func TestReportStaleAssociationVariablesQuietWhenAgreed(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	appID := appOnVersion(t, ctx, ec, "myapp",
		core_v1alpha.ConfigSpecVariables{
			Key: "DATABASE_URL", Value: "postgres://same", Source: coreutil.SourceAddon,
		})
	_, err := ec.Create(ctx, "pg-assoc", &addon_v1alpha.AddonAssociation{
		App:       appID,
		Status:    "active",
		Variables: []addon_v1alpha.Variables{{Key: "DATABASE_URL", Value: "postgres://same"}},
	})
	require.NoError(t, err)

	stale, checked, err := ReportStaleAssociationVariables(ctx, slog.Default(), ec, inmem.EAC)
	require.NoError(t, err)
	assert.Equal(t, 0, stale)
	assert.Equal(t, 1, checked)
}

// TestReportStaleAssociationVariablesIgnoresMissingKey covers the MIR-1579 state
// itself: the app is on a version that never carried the binding, so there is
// nothing to disagree with and nothing to report.
func TestReportStaleAssociationVariablesIgnoresMissingKey(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	appID := appOnVersion(t, ctx, ec, "myapp",
		core_v1alpha.ConfigSpecVariables{Key: "PORT", Value: "3000", Source: coreutil.SourceConfig})
	_, err := ec.Create(ctx, "pg-assoc", &addon_v1alpha.AddonAssociation{
		App:       appID,
		Status:    "active",
		Variables: []addon_v1alpha.Variables{{Key: "DATABASE_URL", Value: "postgres://only-copy"}},
	})
	require.NoError(t, err)

	stale, _, err := ReportStaleAssociationVariables(ctx, slog.Default(), ec, inmem.EAC)
	require.NoError(t, err)
	assert.Equal(t, 0, stale, "a version that simply lacks the key is not a disagreement")
}

// TestReportStaleAssociationVariablesSkipsInactive leaves associations that are
// mid-provision or being torn down out of the count.
func TestReportStaleAssociationVariablesSkipsInactive(t *testing.T) {
	ctx, ec, inmem := setupAssocTest(t)

	appID := appOnVersion(t, ctx, ec, "myapp", core_v1alpha.ConfigSpecVariables{
		Key: "DATABASE_URL", Value: "postgres://rotated", Source: coreutil.SourceAddon,
	})
	_, err := ec.Create(ctx, "pg-assoc", &addon_v1alpha.AddonAssociation{
		App:       appID,
		Status:    "deprovisioning",
		Variables: []addon_v1alpha.Variables{{Key: "DATABASE_URL", Value: "postgres://provisioned"}},
	})
	require.NoError(t, err)

	stale, checked, err := ReportStaleAssociationVariables(ctx, slog.Default(), ec, inmem.EAC)
	require.NoError(t, err)
	assert.Equal(t, 0, stale)
	assert.Equal(t, 0, checked)
}
