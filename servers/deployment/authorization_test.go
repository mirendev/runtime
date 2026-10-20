package deployment

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/rpc"
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
