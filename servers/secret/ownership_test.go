package secret

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

func boundTo(ctx context.Context, app string) context.Context {
	return rpc.ContextWithIdentity(ctx, &rpc.Identity{
		Subject:  "ci",
		Method:   rpc.AuthMethodOIDC,
		Metadata: map[string]any{"bound_app": app},
	})
}

func createApp(t *testing.T, ec *entityserver.Client, name string) entity.Id {
	t.Helper()
	id, err := ec.Create(t.Context(), name, &core_v1alpha.App{OwnedBy: "alice"})
	require.NoError(t, err)
	return id
}

func storedOwner(t *testing.T, ec *entityserver.Client, path string) entity.Id {
	t.Helper()
	var sec core_v1alpha.Secret
	require.NoError(t, ec.Get(t.Context(), entityName(path), &sec))
	return sec.App
}

// entityName mirrors the cluster backend's path-to-name translation, so the
// tests can read the stored row directly.
func entityName(path string) string {
	out := []byte(path)
	for i, c := range out {
		if c == '/' {
			out[i] = '.'
		}
	}
	return string(out)
}

// A secret an app-bound caller creates belongs to that app, and only that app's
// callers can reach it afterwards.
func TestBoundCallerOwnsWhatItCreates(t *testing.T) {
	c, ec := newTestEnv(t)
	ctx := t.Context()
	shop := createApp(t, ec, "shop")
	createApp(t, ec, "other")

	const path = "shop/db-password"
	res, err := c.Set(boundTo(ctx, "shop"), "", path, []byte("hunter2"), "")
	require.NoError(t, err)
	assert.Equal(t, shop, storedOwner(t, ec, path))

	ref := path + "@" + res.Version()

	t.Run("its own app can use it", func(t *testing.T) {
		got, err := c.Resolve(boundTo(ctx, "shop"), "", ref, string(shop))
		require.NoError(t, err)
		assert.Equal(t, []byte("hunter2"), got.Value())
		// Remote runners rely on this to keep the value to shop's sandboxes.
		assert.Equal(t, string(shop), got.OwnerId())

		_, err = c.ListVersions(boundTo(ctx, "shop"), "", path)
		require.NoError(t, err)
	})

	t.Run("another app cannot", func(t *testing.T) {
		other := boundTo(ctx, "other")

		_, err := c.Resolve(other, "", ref, string(shop))
		require.ErrorContains(t, err, "unauthorized")

		_, err = c.Set(other, "", path, []byte("overwritten"), "")
		require.ErrorContains(t, err, "unauthorized")

		_, err = c.SetState(other, "", ref, "disabled")
		require.ErrorContains(t, err, "unauthorized")

		_, err = c.ListVersions(other, "", path)
		require.ErrorContains(t, err, "unauthorized")

		listed, err := c.List(other, "")
		require.NoError(t, err)
		assert.Empty(t, listed.Secrets())

		// The denied write left the value alone.
		got, err := c.Resolve(ctx, "", path, string(shop))
		require.NoError(t, err)
		assert.Equal(t, []byte("hunter2"), got.Value())
	})

	t.Run("plaintext leaves only for the owning app", func(t *testing.T) {
		// A runner that predates ownership names no app, and must not be
		// handed a value it would not know to keep from other sandboxes.
		_, err := c.Resolve(ctx, "", ref, "")
		require.ErrorContains(t, err, "requires naming that app")

		_, err = c.Resolve(ctx, "", ref, "app/other")
		require.ErrorContains(t, err, "belongs to another app")
	})

	t.Run("an unbound caller is not narrowed", func(t *testing.T) {
		got, err := c.Resolve(ctx, "", ref, string(shop))
		require.NoError(t, err)
		assert.Equal(t, []byte("hunter2"), got.Value())

		listed, err := c.List(ctx, "")
		require.NoError(t, err)
		require.Len(t, listed.Secrets(), 1)
		assert.Equal(t, "shop", listed.Secrets()[0].App())

		versions, err := c.ListVersions(ctx, "", path)
		require.NoError(t, err)
		assert.Equal(t, "shop", versions.Secret().App())
	})
}

// Unowned secrets behave exactly as they did before ownership existed, for
// every caller, and a bound caller writing one does not claim it.
func TestUnownedSecretsStayOpen(t *testing.T) {
	c, ec := newTestEnv(t)
	ctx := t.Context()
	createApp(t, ec, "shop")

	const path = "shared/stripe-key"
	_, err := c.Set(ctx, "", path, []byte("sk_live"), "")
	require.NoError(t, err)

	shop := boundTo(ctx, "shop")
	got, err := c.Resolve(shop, "", path, "")
	require.NoError(t, err)
	assert.Equal(t, []byte("sk_live"), got.Value())
	assert.Empty(t, got.OwnerId())

	_, err = c.Set(shop, "", path, []byte("sk_live_2"), "")
	require.NoError(t, err)
	assert.Empty(t, storedOwner(t, ec, path))

	listed, err := c.List(shop, "")
	require.NoError(t, err)
	require.Len(t, listed.Secrets(), 1)
	assert.Empty(t, listed.Secrets()[0].App())
}

// Naming an app on set makes a new secret that app's, and only a caller allowed
// to configure that app can do it.
func TestSetWithAnApp(t *testing.T) {
	c, ec := newTestEnv(t)
	ctx := t.Context()
	shop := createApp(t, ec, "shop")
	createApp(t, ec, "other")

	_, err := c.Set(ctx, "", "shop/db", []byte("v1"), "shop")
	require.NoError(t, err)
	assert.Equal(t, shop, storedOwner(t, ec, "shop/db"))

	t.Run("writing it again for the same app is fine", func(t *testing.T) {
		_, err := c.Set(ctx, "", "shop/db", []byte("v2"), "shop")
		require.NoError(t, err)
	})

	t.Run("naming a different app is refused", func(t *testing.T) {
		_, err := c.Set(ctx, "", "shop/db", []byte("v3"), "other")
		require.ErrorContains(t, err, "belongs to another app")
	})

	t.Run("an existing unowned secret is not claimed", func(t *testing.T) {
		_, err := c.Set(ctx, "", "shared/key", []byte("v1"), "")
		require.NoError(t, err)
		_, err = c.Set(ctx, "", "shared/key", []byte("v2"), "shop")
		require.ErrorContains(t, err, "belongs to no app")
		assert.Empty(t, storedOwner(t, ec, "shared/key"))
	})

	t.Run("the app must exist", func(t *testing.T) {
		_, err := c.Set(ctx, "", "ghost/db", []byte("v1"), "ghost")
		require.ErrorContains(t, err, `app "ghost" not found`)
	})

	t.Run("a bound caller can name only its own app", func(t *testing.T) {
		_, err := c.Set(boundTo(ctx, "shop"), "", "shop/other-db", []byte("v1"), "shop")
		require.NoError(t, err)
		_, err = c.Set(boundTo(ctx, "shop"), "", "other/db", []byte("v1"), "other")
		require.ErrorContains(t, err, "unauthorized")
	})

	t.Run("under Cloud authorization the caller needs the app", func(t *testing.T) {
		cctx := rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "bob", Method: rpc.AuthMethodJWT})
		cctx = rpc.ContextWithAuthorization(cctx, &appPolicy{readable: map[string]bool{"shop": true}}, "secrets", "set")

		_, err := c.Set(cctx, "", "shop/cloud-db", []byte("v1"), "shop")
		require.NoError(t, err)
		_, err = c.Set(cctx, "", "other/cloud-db", []byte("v1"), "other")
		require.ErrorContains(t, err, "unauthorized")
	})
}

// appPolicy is a Cloud-style resource authorizer that allows only the listed
// apps, for any action.
type appPolicy struct {
	readable map[string]bool
}

func (*appPolicy) Authorize(context.Context, *rpc.Identity, string, string) error { return nil }

func (p *appPolicy) AuthorizeResource(_ context.Context, _ *rpc.Identity, req rpc.ResourceRequest) error {
	if req.Resource == "app" && p.readable[req.Instance] {
		return nil
	}
	return rpc.ErrResourceDenied
}

// Under Cloud authorization, an operator reaches an owned secret only through
// read on its app, while unowned secrets stay open.
func TestLogicalAuthorizationChecksTheOwningApp(t *testing.T) {
	c, ec := newTestEnv(t)
	ctx := t.Context()
	shopID := createApp(t, ec, "shop")
	createApp(t, ec, "other")

	_, err := c.Set(boundTo(ctx, "shop"), "", "shop/token", []byte("a"), "")
	require.NoError(t, err)
	_, err = c.Set(boundTo(ctx, "other"), "", "other/token", []byte("b"), "")
	require.NoError(t, err)
	_, err = c.Set(ctx, "", "shared/token", []byte("c"), "")
	require.NoError(t, err)

	caller := func(method string) context.Context {
		cctx := rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "bob", Method: rpc.AuthMethodJWT})
		return rpc.ContextWithAuthorization(cctx, &appPolicy{readable: map[string]bool{"shop": true}}, "secrets", method)
	}

	_, err = c.Resolve(caller("resolve"), "", "shop/token", string(shopID))
	require.NoError(t, err)
	_, err = c.Resolve(caller("resolve"), "", "shared/token", "")
	require.NoError(t, err)
	_, err = c.Resolve(caller("resolve"), "", "other/token", "")
	require.ErrorContains(t, err, "unauthorized")

	listed, err := c.List(caller("list"), "")
	require.NoError(t, err)
	var paths []string
	for _, s := range listed.Secrets() {
		paths = append(paths, s.Path())
	}
	assert.ElementsMatch(t, []string{"shop/token", "shared/token"}, paths)
}
