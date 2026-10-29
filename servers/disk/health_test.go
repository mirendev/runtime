package disk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
	entitysrv "miren.dev/runtime/servers/entityserver"
)

func TestDiskHealthRejectsCorruptPage(t *testing.T) {
	for _, session := range []bool{false, true} {
		t.Run(fmt.Sprintf("session=%t", session), func(t *testing.T) {
			ctx := t.Context()
			etcd, prefix := etcdtest.TestEtcdClient(t)
			log := testutils.TestLogger(t)
			store, err := entity.NewEtcdStore(ctx, log, etcd, prefix)
			require.NoError(t, err)
			require.NoError(t, schema.Apply(ctx, store))
			server, err := entitysrv.NewEntityServer(log, store)
			require.NoError(t, err)
			eac := entityserver_v1alpha.NewEntityAccessClient(rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)))
			ec := entityserver.NewClient(log, eac)
			_, err = ec.Create(ctx, "healthy", &storage_v1alpha.DiskVolume{ActualState: storage_v1alpha.DV_READY})
			require.NoError(t, err)
			id, err := ec.Create(ctx, "corrupt", &storage_v1alpha.DiskVolume{ActualState: storage_v1alpha.DV_ERROR})
			require.NoError(t, err)
			key := prefix + "/entity/" + base58.Encode([]byte(id))
			if session {
				key += "/session/corrupt"
			}
			_, err = etcd.Put(ctx, key, "\xff invalid cbor credential-canary")
			require.NoError(t, err)
			// Ordinary entity listing remains tolerant and still returns the
			// readable record; diagnostics must not mistake that for a full scan.
			page, err := eac.ListPage(ctx, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskVolume), "", 200)
			require.NoError(t, err)
			require.Len(t, page.Values(), 1)
			require.True(t, page.Incomplete())
			client := disk_v1alpha.NewDiskHealthClient(rpc.LocalClient(disk_v1alpha.AdaptDiskHealth(NewHealthServer(log, eac))))
			_, err = client.List(ctx, "disk_volume", "", 200)
			require.EqualError(t, err, "resource health scan unavailable")
		})
	}
}

func TestDiskHealthPagesSafeLifecycleSummaries(t *testing.T) {
	ctx := t.Context()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	client := disk_v1alpha.NewDiskHealthClient(rpc.LocalClient(disk_v1alpha.AdaptDiskHealth(NewHealthServer(testutils.TestLogger(t), inmem.EAC))))

	empty, err := client.List(ctx, "disk_volume", "", 0)
	require.NoError(t, err)
	assert.Empty(t, empty.Resources())
	assert.Empty(t, empty.Cursor())

	want := map[string]string{}
	for i := range 203 {
		status := storage_v1alpha.ATTACHED
		if i == 201 {
			status = storage_v1alpha.ERROR
		} else if i == 0 {
			status = storage_v1alpha.RESTORING
		}
		id, err := inmem.Client.Create(ctx, fmt.Sprintf("disk-%03d", i), &storage_v1alpha.Disk{Name: "disk", Status: status})
		require.NoError(t, err)
		want[id.String()] = string(status)
	}
	for _, limit := range []int64{0, 9999, 1} {
		got := map[string]string{}
		first := true
		for cursor := ""; ; {
			page, err := client.List(ctx, "disk", cursor, limit)
			require.NoError(t, err)
			if first && limit != 1 {
				assert.Len(t, page.Resources(), 200)
				assert.NotEmpty(t, page.Cursor())
			}
			first = false
			for _, item := range page.Resources() {
				assert.NotContains(t, got, item.Id(), "page must advance without duplicates")
				got[item.Id()] = item.State()
				assert.Equal(t, item.Id(), item.DiskId())
				assert.Equal(t, item.State() == "status.error", item.Diagnostic() != "")
			}
			cursor = page.Cursor()
			if cursor == "" {
				break
			}
		}
		assert.Equal(t, want, got)
	}

	const secret = "credential-canary-must-not-leak"
	volume, err := inmem.Client.Create(ctx, "bad-volume", &storage_v1alpha.DiskVolume{
		DiskId: "disk/parent", ActualState: storage_v1alpha.DV_ERROR,
		ErrorMessage: "failed to open /private/" + secret, ImagePath: "/private/" + secret,
		CloudVolumeId: secret, MountId: secret,
	})
	require.NoError(t, err)
	_, err = inmem.Client.Create(ctx, "ready-volume", &storage_v1alpha.DiskVolume{DiskId: "disk/other", ActualState: storage_v1alpha.DV_READY, ErrorMessage: secret})
	require.NoError(t, err)
	page, err := client.List(ctx, "disk_volume", "", 10)
	require.NoError(t, err)
	require.Len(t, page.Resources(), 2)
	for _, item := range page.Resources() {
		if item.Id() == volume.String() {
			assert.Equal(t, "disk/parent", item.DiskId())
			assert.Equal(t, "actual_state.dv_error", item.State())
			assert.Equal(t, "volume lifecycle failed; inspect server logs", item.Diagnostic())
		} else {
			assert.Equal(t, "actual_state.dv_ready", item.State())
			assert.Empty(t, item.Diagnostic())
		}
	}
	wire, err := json.Marshal(page.Resources())
	require.NoError(t, err)
	assert.NotContains(t, string(wire), secret)
	assert.NotContains(t, string(wire), "image_path")
	assert.NotContains(t, string(wire), "error_message")

	for _, args := range []struct {
		kind, cursor string
		limit        int64
	}{{"app", "", 1}, {"disk", "", -1}, {"disk", "0!", 1}, {"disk", strings.Repeat("a", 4097), 1}} {
		_, err := client.List(ctx, args.kind, args.cursor, args.limit)
		require.Error(t, err)
	}
	inmem.Store.GetEntitiesFunc = func(context.Context, []entity.Id) ([]*entity.Entity, error) {
		return nil, errors.New(secret)
	}
	_, err = client.List(ctx, "disk", "", 1)
	require.EqualError(t, err, "resource health scan unavailable")
}

func TestDiskHealthAccessThroughPublicGateway(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	_, err := inmem.Client.Create(t.Context(), "bad", &storage_v1alpha.Disk{Status: storage_v1alpha.ERROR})
	require.NoError(t, err)
	state, err := rpc.NewState(t.Context(), rpc.WithSkipVerify,
		rpc.WithRESTBindAddr("localhost:0"),
		rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}),
		rpc.WithAuthorizer(oidcauth.NewCompositeAuthorizer(nil)))
	require.NoError(t, err)
	state.Server().ExposeValue("dev.miren.runtime/disk-health", disk_v1alpha.AdaptDiskHealth(NewHealthServer(testutils.TestLogger(t), inmem.EAC)))

	for _, tc := range []struct {
		name string
		id   *rpc.Identity
		code int
	}{
		{"unauthenticated", nil, http.StatusUnauthorized},
		{"operator", &rpc.Identity{Method: rpc.AuthMethodJWT}, http.StatusOK},
		{"internal", &rpc.Identity{Method: rpc.AuthMethodCert}, http.StatusOK},
		{"runner certificate", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-01234567-89ab-cdef-0123-456789abcdef"}, http.StatusForbidden},
		{"cluster reader", &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": workloadroles.RoleClusterReadonly}}, http.StatusOK},
		{"cluster admin", &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": workloadroles.RoleClusterAdmin}}, http.StatusOK},
		{"own app", &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": workloadroles.RoleAppAdmin, "app": "bad"}}, http.StatusForbidden},
		{"other app", &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": workloadroles.RoleAppReadonly, "app": "other"}}, http.StatusForbidden},
		{"OIDC", &rpc.Identity{Method: rpc.AuthMethodOIDC, Metadata: map[string]any{"bound_app": "bad"}}, http.StatusForbidden},
		{"system", &rpc.Identity{Method: rpc.AuthMethodSystem}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/disk-health/disk", nil)
			if tc.id != nil {
				req = req.WithContext(rpc.ContextWithIdentity(req.Context(), tc.id))
			}
			resp := httptest.NewRecorder()
			state.Server().ServeHTTP(resp, req)
			require.Equal(t, tc.code, resp.Code, resp.Body.String())
			if tc.code == http.StatusOK {
				assert.Contains(t, resp.Body.String(), "status.error")
			} else {
				assert.NotContains(t, resp.Body.String(), "status.error")
			}
		})
	}

	// Even a direct invocation bypassing method policy cannot leak to an app.
	client := disk_v1alpha.NewDiskHealthClient(rpc.LocalClient(disk_v1alpha.AdaptDiskHealth(NewHealthServer(testutils.TestLogger(t), inmem.EAC))))
	ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Method: rpc.AuthMethodOIDC, Metadata: map[string]any{"bound_app": "bad"}})
	_, err = client.List(ctx, "disk", "", 1)
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
}
