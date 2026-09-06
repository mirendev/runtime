package ingress

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	entityapi "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	entityserver "miren.dev/runtime/servers/entityserver"
)

func TestDefaultRouteAppUpdateRepairsOnlyAppDefault(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, nil))
	store := entity.NewMockStore()
	server := &entityserver.EntityServer{Log: log, Store: store}
	client := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server))
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityapi.NewClient(log, eac)

	app := &core_v1alpha.App{}
	appID, err := ec.Create(context.Background(), "only", app)
	require.NoError(t, err)
	app.ID = appID

	controller := NewDefaultRouteAppController(log, client)
	meta := &entity.Meta{Entity: entity.New(entity.DBId, appID)}
	require.NoError(t, controller.Update(t.Context(), app, meta))

	route, err := controller.ic.LookupDefault(t.Context())
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, appID, route.App)
	before, err := eac.Get(t.Context(), route.ID.String())
	require.NoError(t, err)

	// Re-checking the snapshot invariant must not rewrite an already-correct route.
	require.NoError(t, controller.Update(t.Context(), app, meta))
	routes, err := eac.List(t.Context(), entity.Bool(ingress_v1alpha.HttpRouteDefaultId, true))
	require.NoError(t, err)
	assert.Len(t, routes.Values(), 1)
	after, err := eac.Get(t.Context(), route.ID.String())
	require.NoError(t, err)
	assert.Equal(t, before.Entity().Revision(), after.Entity().Revision())
}
