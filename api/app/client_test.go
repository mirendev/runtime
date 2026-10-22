package app_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/rpc"
)

func TestCreatePreservesFirstCreator(t *testing.T) {
	inmem, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	_, err := inmem.Client.Create(t.Context(), "default", &core_v1alpha.Project{})
	require.NoError(t, err)
	client := app.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))

	const callers = 8
	start := make(chan struct{})
	var workers sync.WaitGroup
	results := make([]*core_v1alpha.App, callers)
	errs := make([]error, callers)
	for i := range callers {
		workers.Go(func() {
			ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: fmt.Sprintf("creator-%d", i), Method: rpc.AuthMethodJWT})
			<-start
			results[i], errs[i] = client.Create(ctx, "shared")
		})
	}
	close(start)
	workers.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	stored, err := client.GetByName(t.Context(), "shared")
	require.NoError(t, err)
	require.Regexp(t, `^creator-[0-7]$`, stored.OwnedBy)
	for _, result := range results {
		assert.Equal(t, stored.ID, result.ID)
		assert.Equal(t, stored.OwnedBy, result.OwnedBy)
	}
}

func TestCreateDoesNotClaimUnownedApps(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	client := app.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))
	_, err := inmem.Client.Create(t.Context(), "legacy", &core_v1alpha.App{WorkloadRole: "app-deployer"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		method rpc.AuthMethod
	}{
		{name: "legacy"},
		{name: "unattributed"},
		{name: "anonymous", method: rpc.AuthMethodAnonymous},
		{name: "cert", method: rpc.AuthMethodCert},
		{name: "oidc", method: rpc.AuthMethodOIDC},
		{name: "workload", method: rpc.AuthMethodWorkload},
		{name: "system", method: rpc.AuthMethodSystem},
		{name: "token", method: rpc.AuthMethodToken},
		{name: "signed", method: rpc.AuthMethodSigned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.method != "" {
				ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "later-deployer", Method: tc.method})
			}
			_, err := client.Create(ctx, tc.name)
			require.NoError(t, err)
			ctx = rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: "later-deployer", Method: rpc.AuthMethodJWT})
			record, err := client.Create(ctx, tc.name)
			require.NoError(t, err)
			assert.Empty(t, record.OwnedBy)
			stored, err := client.GetByName(t.Context(), tc.name)
			require.NoError(t, err)
			assert.Empty(t, stored.OwnedBy)
			if tc.name == "legacy" {
				assert.Equal(t, "app-deployer", stored.WorkloadRole)
			}
		})
	}
}

type appPolicyAuthorizer struct {
	policy    rbac.Policy
	evaluator *rbac.Evaluator
}

func (a *appPolicyAuthorizer) GetPolicy() *rbac.Policy { return &a.policy }
func (*appPolicyAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return nil
}
func (a *appPolicyAuthorizer) AuthorizeResource(_ context.Context, identity *rpc.Identity, request rpc.ResourceRequest) error {
	if identity != nil && a.evaluator.Evaluate(&rbac.Request{
		Subject: identity.Subject, Groups: []string{"members"}, Resource: request.Resource,
		Action: request.Action, Instance: request.Instance, Owner: request.Owner,
	}) == rbac.DecisionAllow {
		return nil
	}
	return rpc.ErrUnauthorized
}

func TestAppAndSandboxPermissionsResolveStoredOwnership(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	auth := &appPolicyAuthorizer{policy: rbac.Policy{Rules: []rbac.Rule{{Groups: []string{"members"}, Permissions: []rbac.Permission{
		{Resource: "app", Actions: []string{"read", "exec"}, Scope: "$owner"},
		{Resource: "app", Actions: []string{"logs"}, Scope: "shared-*"},
	}}}}}
	auth.evaluator = rbac.NewEvaluator(t.Context(), auth, slog.Default())
	ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT})
	ctx = rpc.ContextWithAuthorization(ctx, auth, "appstatus", "appinfo")
	for _, name := range []string{"mine", "theirs", "shared-blog"} {
		owner := "bob"
		if name == "mine" {
			owner = "alice"
		}
		appID, err := inmem.Client.Create(t.Context(), name, &core_v1alpha.App{OwnedBy: owner})
		require.NoError(t, err)
		versionID, err := inmem.Client.Create(t.Context(), name+"-v1", &core_v1alpha.AppVersion{App: appID})
		require.NoError(t, err)
		var sandbox compute_v1alpha.Sandbox
		sandbox.Spec.Version = versionID
		sandboxID, err := inmem.Client.Create(t.Context(), name+"-sb", &sandbox)
		require.NoError(t, err)
		for _, action := range []string{"read", "exec", "logs", "configure"} {
			allowed := (name == "mine" && (action == "read" || action == "exec")) || (name == "shared-blog" && action == "logs")
			for _, target := range []string{name, appID.String()} {
				err = app.Authorize(ctx, inmem.EAC, target, action)
				if allowed {
					require.NoError(t, err, "%s %s", target, action)
				} else {
					require.ErrorIs(t, err, rpc.ErrUnauthorized, "%s %s", target, action)
				}
			}
			err = app.AuthorizeSandbox(ctx, inmem.EAC, sandboxID.String(), action)
			if allowed {
				require.NoError(t, err, "%s %s", sandboxID, action)
			} else {
				require.ErrorIs(t, err, rpc.ErrUnauthorized, "%s %s", sandboxID, action)
			}
		}
	}
	require.ErrorIs(t, app.Authorize(ctx, inmem.EAC, "mine", "create"), rpc.ErrUnauthorized)
	_, err := inmem.Client.Create(t.Context(), "orphan", &compute_v1alpha.Sandbox{})
	require.NoError(t, err)
	require.ErrorIs(t, app.AuthorizeSandbox(ctx, inmem.EAC, "sandbox/orphan", "exec"), rpc.ErrUnauthorized)
	var mine core_v1alpha.App
	var foreign core_v1alpha.AppVersion
	require.NoError(t, inmem.Client.Get(t.Context(), "mine", &mine))
	require.NoError(t, inmem.Client.Get(t.Context(), "theirs-v1", &foreign))
	poolID, err := inmem.Client.Create(t.Context(), "mine-web", &compute_v1alpha.SandboxPool{App: mine.ID})
	require.NoError(t, err)
	var pooled compute_v1alpha.Sandbox
	pooled.Spec.Version = foreign.ID
	pooledID, err := inmem.Client.Create(t.Context(), "pool-owned", &pooled,
		entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	require.NoError(t, err)
	require.NoError(t, app.AuthorizeSandbox(ctx, inmem.EAC, pooledID.String(), "exec"), "pool ownership must take precedence over version attribution")
	require.NoError(t, inmem.Client.Delete(t.Context(), poolID))
	require.ErrorIs(t, app.AuthorizeSandbox(ctx, inmem.EAC, pooledID.String(), "exec"), rpc.ErrUnauthorized)
	operator := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: "operator", Method: rpc.AuthMethodCert})
	operator = rpc.ContextWithAuthorization(operator, oidcauth.NewCompositeAuthorizer(auth), "sandboxexec", "exec")
	require.NoError(t, app.AuthorizeSandbox(operator, inmem.EAC, pooledID.String(), "exec"))
	missingVersionID, err := inmem.Client.Create(t.Context(), "missing-version", &compute_v1alpha.Sandbox{
		Spec: compute_v1alpha.SandboxSpec{Version: "app_version/missing"},
	})
	require.NoError(t, err)
	require.ErrorIs(t, app.AuthorizeSandbox(ctx, inmem.EAC, missingVersionID.String(), "exec"), rpc.ErrUnauthorized)
	require.NoError(t, app.AuthorizeSandbox(operator, inmem.EAC, missingVersionID.String(), "exec"))
	var shared core_v1alpha.App
	require.NoError(t, inmem.Client.Get(t.Context(), "shared-blog", &shared))
	require.NoError(t, inmem.Client.Delete(t.Context(), shared.ID))
	require.ErrorIs(t, app.AuthorizeSandbox(ctx, inmem.EAC, "sandbox/shared-blog-sb", "logs"), rpc.ErrUnauthorized,
		"a named app grant must not stand in for an unresolved sandbox owner")
	require.NoError(t, app.AuthorizeSandbox(operator, inmem.EAC, "sandbox/shared-blog-sb", "logs"))
}

type failingAppLookup struct {
	entityserver_v1alpha.EntityAccess
	err error
}

func (s failingAppLookup) Get(context.Context, *entityserver_v1alpha.EntityAccessGet) error {
	return s.err
}

func TestAppAuthorizationLookupErrorsAndMissingTargets(t *testing.T) {
	auth := &appPolicyAuthorizer{policy: rbac.Policy{Rules: []rbac.Rule{{Groups: []string{"members"}, Permissions: []rbac.Permission{
		{Resource: "app", Actions: []string{"configure", "logs"}, Scope: "explicit"},
		{Resource: "app", Actions: []string{"configure", "logs"}, Scope: "$owner"},
	}}}}}
	auth.evaluator = rbac.NewEvaluator(t.Context(), auth, slog.Default())
	ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT})
	ctx = rpc.ContextWithAuthorization(ctx, auth, "crud", "setconfiguration")
	for _, failure := range []error{context.Canceled, fmt.Errorf("entity store unavailable")} {
		eac := entityserver_v1alpha.NewEntityAccessClient(rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(failingAppLookup{err: failure})))
		require.ErrorIs(t, app.Authorize(ctx, eac, "explicit", "configure"), failure)
		require.ErrorIs(t, app.AuthorizeSandbox(ctx, eac, "sandbox/explicit", "logs"), failure)
	}
	eac := entityserver_v1alpha.NewEntityAccessClient(rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(failingAppLookup{err: cond.NotFound("app", "missing")})))
	for _, action := range []string{"configure", "logs"} {
		require.NoError(t, app.Authorize(ctx, eac, "explicit", action), "explicit grants preserve handler-specific missing-app behavior")
		require.ErrorIs(t, app.Authorize(ctx, eac, "missing", action), rpc.ErrUnauthorized, "$owner must not match a missing app")
	}
}
