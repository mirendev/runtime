package deployment

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// The deployment env-var writes gained rpc.AllowApp guards so they can be part
// of an app-scoped role without letting a workload write to another app. An
// identity bound to "other-app" must be refused when it targets "victim".
func TestDeploymentEnvWrites_AppScopingGuards(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("create deployment server: %v", err)
	}

	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	otherApp := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject:  "org:o:app:other-app:sandbox:sb-1",
		Method:   rpc.AuthMethodWorkload,
		Metadata: map[string]any{"app": "other-app"},
	})

	envVar := &deployment_v1alpha.EnvironmentVariable{}
	envVar.SetKey("K")
	envVar.SetValue("v")

	writes := map[string]func(context.Context) error{
		"SetEnvVars": func(c context.Context) error {
			_, err := client.SetEnvVars(c, "victim", "cluster-1", []*deployment_v1alpha.EnvironmentVariable{envVar}, "")
			return err
		},
		"DeleteEnvVars": func(c context.Context) error {
			_, err := client.DeleteEnvVars(c, "victim", "cluster-1", []string{"K"}, "")
			return err
		},
	}

	for name, call := range writes {
		t.Run(name, func(t *testing.T) {
			if err := call(otherApp); !errors.Is(err, rpc.ErrUnauthorized) {
				t.Fatalf("expected ErrUnauthorized targeting another app, got %v", err)
			}
		})
	}
}

// The AllowApp guards confine the target app but not the source version. A
// caller authorized for appX must not be able to deploy appY's built version
// into appX (which would pull appY's image/config, and any secrets baked in).
// This holds regardless of auth method — the confinement is on the version's
// ownership, checked server-side.
func TestDeploy_RejectsCrossAppVersion(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("create deployment server: %v", err)
	}
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	// Two apps, each with its own version (App set as the build would).
	appXID, err := inmem.Client.Create(ctx, "app-x", &core_v1alpha.App{})
	if err != nil {
		t.Fatalf("create app-x: %v", err)
	}
	appYID, err := inmem.Client.Create(ctx, "app-y", &core_v1alpha.App{})
	if err != nil {
		t.Fatalf("create app-y: %v", err)
	}
	appXVersionID, err := inmem.Client.Create(ctx, "app-x-v1", &core_v1alpha.AppVersion{Version: "app-x-v1", App: appXID})
	if err != nil {
		t.Fatalf("create app-x version: %v", err)
	}
	appYVersionID, err := inmem.Client.Create(ctx, "app-y-v1", &core_v1alpha.AppVersion{Version: "app-y-v1", App: appYID})
	if err != nil {
		t.Fatalf("create app-y version: %v", err)
	}

	t.Run("DeployVersion rejects another app's version", func(t *testing.T) {
		for _, versionRef := range []string{"app-y-v1", string(appYVersionID)} {
			res, err := client.DeployVersion(ctx, "app-x", "cluster-1", versionRef, false, nil, "", "")
			// The mismatch surfaces as a hard error (ValidationFailure), not a
			// results.Error field.
			if err == nil {
				if res != nil && res.HasError() {
					t.Fatalf("ref %q: expected a hard error, got results.Error=%q", versionRef, res.Error())
				}
				t.Fatalf("ref %q: expected cross-app version to be rejected", versionRef)
			}
			if !strings.Contains(err.Error(), "does not belong to app") {
				t.Fatalf("ref %q: expected ownership error, got %v", versionRef, err)
			}
		}
	})

	t.Run("UpdateDeploymentAppVersion rejects another app's version", func(t *testing.T) {
		depID, err := inmem.Client.Create(ctx, "dep-x", &core_v1alpha.Deployment{
			AppName:    "app-x",
			ClusterId:  "cluster-1",
			AppVersion: "app-x-v1",
			Status:     "succeeded",
		})
		if err != nil {
			t.Fatalf("create deployment: %v", err)
		}

		for _, versionRef := range []string{"app-y-v1", string(appYVersionID)} {
			_, err = client.UpdateDeploymentAppVersion(ctx, string(depID), versionRef)
			if err == nil || !strings.Contains(err.Error(), "does not belong to app") {
				t.Fatalf("ref %q: expected ownership error, got %v", versionRef, err)
			}
		}
	})

	t.Run("own version is accepted past the ownership check", func(t *testing.T) {
		// app-x deploying app-x-v1 must clear the ownership check (it may still
		// fail later for unrelated reasons, but never with a mismatch error).
		res, err := client.DeployVersion(ctx, "app-x", "cluster-1", string(appXVersionID), false, nil, "", "")
		if err != nil && strings.Contains(err.Error(), "does not belong to app") {
			t.Fatalf("own version was wrongly rejected: %v", err)
		}
		_ = res
	})
}
