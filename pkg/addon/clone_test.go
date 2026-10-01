package addon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestRequestClonesCreatesVersionScopedAssociationsIdempotently(t *testing.T) {
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := t.Context()
	appID, err := es.Client.Create(ctx, "app", &core_v1alpha.App{})
	require.NoError(t, err)
	versionID, err := es.Client.Create(ctx, "preview", &core_v1alpha.AppVersion{App: appID, EphemeralLabel: "preview"})
	require.NoError(t, err)
	sourceID, err := es.Client.Create(ctx, "source", &addon_v1alpha.AddonAssociation{
		App: appID, Addon: "addon/miren-postgresql", Variant: "small", Version: "17",
		Services: []string{"web"}, Status: "active",
	})
	require.NoError(t, err)

	require.NoError(t, RequestClones(ctx, es.EAC, appID, versionID))
	require.NoError(t, RequestClones(ctx, es.EAC, appID, versionID))
	resp, err := es.EAC.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppVersionId, versionID))
	require.NoError(t, err)
	require.Len(t, resp.Values(), 1)
	var clone addon_v1alpha.AddonAssociation
	clone.Decode(resp.Values()[0].Entity())
	assert.Equal(t, sourceID, clone.SourceAssociation)
	assert.Equal(t, entity.Id("addon/miren-postgresql"), clone.Addon)
	assert.Equal(t, "small", clone.Variant)
	assert.Equal(t, []string{"web"}, clone.Services)
	assert.Equal(t, "pending", clone.Status)
}

func TestRequestClonesRejectsUnreadySource(t *testing.T) {
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := t.Context()
	appID, err := es.Client.Create(ctx, "app", &core_v1alpha.App{})
	require.NoError(t, err)
	_, err = es.Client.Create(ctx, "source", &addon_v1alpha.AddonAssociation{App: appID, Status: "provisioning"})
	require.NoError(t, err)

	err = RequestClones(ctx, es.EAC, appID, "app_version/preview")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready to clone")
}
