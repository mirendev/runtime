package deployment

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
)

type deploymentPolicy struct {
	app, action string
}

func (*deploymentPolicy) Authorize(context.Context, *rpc.Identity, string, string) error { return nil }
func (p *deploymentPolicy) AuthorizeResource(_ context.Context, _ *rpc.Identity, r rpc.ResourceRequest) error {
	if r.Instance == p.app && r.Action == p.action && r.Owner == "owner" {
		return nil
	}
	return rpc.ErrUnauthorized
}

func TestDeploymentResourceAuthorizationBeforeMutation(t *testing.T) {
	client, inmem := newLockTestClient(t)
	_, _, err := app.EnsureApp(context.Background(), inmem.EAC, "web", "owner")
	require.NoError(t, err)
	rec := beginLockTestDeployment(t, inmem, "web", "prod")
	tracker := deploylifecycle.NewTracker(slog.Default(), inmem.EAC)
	for _, grant := range []string{"other", "web"} {
		policy := &deploymentPolicy{app: grant, action: "deploy"}
		ctx := rpc.ContextWithAuthorization(context.Background(), policy, "deployment", "updatedeploymentphase")
		_, err := client.UpdateDeploymentPhase(ctx, rec.Deployment.ID.String(), "building")
		if grant == "other" {
			require.ErrorIs(t, err, rpc.ErrUnauthorized)
			unchanged, err := tracker.Store().Get(context.Background(), rec.Deployment.ID.String())
			require.NoError(t, err)
			require.Equal(t, rec.Deployment.Phase, unchanged.Deployment.Phase)
		} else {
			require.NoError(t, err)
		}
	}
	policy := &deploymentPolicy{app: "web", action: "deploy"}
	ctx := rpc.ContextWithAuthorization(context.Background(), policy, "deployment", "setenvvars")
	variable := &deployment_v1alpha.EnvironmentVariable{}
	variable.SetKey("K")
	variable.SetValue("v")
	_, err = client.SetEnvVars(ctx, "web", "prod", []*deployment_v1alpha.EnvironmentVariable{variable}, "")
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
}

func TestLegacyDeploymentAuthorizationUsesStoredApp(t *testing.T) {
	client, inmem := newLockTestClient(t)
	_, _, err := app.EnsureApp(t.Context(), inmem.EAC, "web", "owner")
	require.NoError(t, err)
	legacyID, err := inmem.Client.Create(t.Context(), "legacy", &core_v1alpha.Deployment{AppName: "web", Status: "active"})
	require.NoError(t, err)
	for _, grant := range []string{"other", "web"} {
		ctx := rpc.ContextWithAuthorization(t.Context(), &deploymentPolicy{app: grant, action: "read"}, "deployment", "getdeploymentbyid")
		result, err := client.GetDeploymentById(ctx, legacyID.String())
		if grant == "other" {
			require.ErrorIs(t, err, rpc.ErrUnauthorized)
		} else {
			require.NoError(t, err)
			require.Equal(t, "web", result.Deployment().AppName())
		}
	}
	ctx := rpc.ContextWithAuthorization(t.Context(), &deploymentPolicy{app: "web", action: "deploy"}, "deployment", "updatedeploymentphase")
	_, err = client.UpdateDeploymentPhase(ctx, legacyID.String(), "building")
	require.ErrorContains(t, err, "cannot set phase on deployment in succeeded state")
}

func TestBoundCallersAuthorizeCanonicalDeployments(t *testing.T) {
	for _, identity := range []*rpc.Identity{
		{Method: rpc.AuthMethodOIDC, Metadata: map[string]any{"bound_app": "web"}},
		{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "web", "role": workloadroles.RoleAppDeployer}},
		{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "web", "role": workloadroles.RoleAppAdmin}},
	} {
		t.Run(string(identity.Method)+fmt.Sprint(identity.Metadata["role"]), func(t *testing.T) {
			client, inmem := newLockTestClient(t)
			tracker := deploylifecycle.NewTracker(slog.Default(), inmem.EAC)
			auth := oidcauth.NewCompositeAuthorizer(nil)
			for _, name := range []string{"web", "other"} {
				app, _, err := app.EnsureApp(t.Context(), inmem.EAC, name, "owner")
				require.NoError(t, err)
				record, err := tracker.Begin(t.Context(), deploylifecycle.BeginParams{
					AppName: name, AppID: app.ID, ClusterID: "prod", Operation: deploylifecycle.OperationBuild,
				})
				require.NoError(t, err)
				require.Equal(t, app.ID, record.Deployment.App)
				ctx := rpc.ContextWithIdentity(t.Context(), identity)
				ctx = rpc.ContextWithAuthorization(ctx, auth, "deployment", "getdeploymentbyid")
				require.NoError(t, auth.Authorize(ctx, identity, "deployment", "getdeploymentbyid"))
				result, err := client.GetDeploymentById(ctx, record.Deployment.ID.String())
				if name == "web" {
					require.NoError(t, err)
					require.Equal(t, name, result.Deployment().AppName())
				} else {
					require.ErrorIs(t, err, rpc.ErrUnauthorized)
				}
				ctx = rpc.ContextWithAuthorization(ctx, auth, "deployment", "canceldeployment")
				require.NoError(t, auth.Authorize(ctx, identity, "deployment", "canceldeployment"))
				canceled, err := client.CancelDeployment(ctx, record.Deployment.ID.String(), "")
				if name == "web" {
					require.NoError(t, err)
					require.True(t, canceled.Success())
				} else {
					require.ErrorIs(t, err, rpc.ErrUnauthorized)
					unchanged, err := tracker.Store().Get(t.Context(), record.Deployment.ID.String())
					require.NoError(t, err)
					require.Equal(t, deploylifecycle.StatusInProgress, unchanged.Status())
				}
			}
			misattributed, err := inmem.Client.Create(t.Context(), "misattributed", &core_v1alpha.Deployment{
				AppName: "web", App: "app/other", Status: "active",
			})
			require.NoError(t, err)
			ctx := rpc.ContextWithIdentity(t.Context(), identity)
			ctx = rpc.ContextWithAuthorization(ctx, auth, "deployment", "getdeploymentbyid")
			_, err = client.GetDeploymentById(ctx, misattributed.String())
			require.ErrorIs(t, err, rpc.ErrUnauthorized, "the stored app reference must win over a conflicting display name")
		})
	}
}
