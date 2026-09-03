package deployment

import (
	"context"
	"log/slog"
	"testing"
	"time"

	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// TestUnmigratedLegacyActiveDroppedByListAndGet pins the migration-window fix
// alongside it. When app.active_deployment is empty but a single retained
// legacy "active" deployment is the serving row, ListDeployments and
// GetDeploymentById must render that row "active" — consistent with
// GetActiveDeployment, which already serves it via its legacy fallback —
// rather than collapsing it to the canonical "succeeded".
//
// The two substates span the whole migration window until the controller's
// apps phase backfills app.active_deployment:
//   - pre_canonical: only raw legacy fields are set (t=0 of the window).
//   - post_canonical: migrateDeployment has set Outcome/App/Version/StartedAt
//     but the raw dep.Status is still "active", and the pointer is still empty.
//
// Regression: before the fix, ListDeployments and GetDeploymentById computed
// the serving flag solely from app.active_deployment, so for an empty pointer
// the served row rendered "succeeded" while GetActiveDeployment rendered
// "active". That split is what this test forbids.
func TestUnmigratedLegacyActiveDroppedByListAndGet(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		dep  func() *core_v1alpha.Deployment
	}{
		{
			name: "pre_canonical",
			dep: func() *core_v1alpha.Deployment {
				return &core_v1alpha.Deployment{
					AppName:    "legacy-serve-app",
					ClusterId:  "test-cluster",
					AppVersion: "legacy-serve-v1",
					Status:     "active",
					DeployedBy: core_v1alpha.DeployedBy{Timestamp: now.Add(-time.Hour).Format(time.RFC3339)},
				}
			},
		},
		{
			name: "post_canonical",
			dep: func() *core_v1alpha.Deployment {
				return &core_v1alpha.Deployment{
					AppName:    "legacy-serve-app",
					ClusterId:  "test-cluster",
					AppVersion: "legacy-serve-v1",
					Status:     "active",
					Outcome:    "succeeded",
					App:        "app/legacy-serve-app",
					Version:    "app_version/legacy-serve-v1",
					StartedAt:  now.Add(-time.Hour),
					DeployedBy: core_v1alpha.DeployedBy{Timestamp: now.Add(-time.Hour).Format(time.RFC3339)},
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

			servedID, err := inmem.Client.Create(ctx, "legacy-serve-dep", tc.dep())
			if err != nil {
				t.Fatalf("create served deployment: %v", err)
			}

			// The app exists, but the migration has not backfilled
			// app.active_deployment yet — the exact empty-pointer state the bug
			// rides on.
			if _, err := inmem.Client.Create(ctx, "legacy-serve-app", &core_v1alpha.App{}); err != nil {
				t.Fatalf("create app: %v", err)
			}

			// GetActiveDeployment is the source of truth and already renders
			// the row "active" via its legacy fallback; the other two RPCs must
			// not disagree with it.
			active, err := client.GetActiveDeployment(ctx, "legacy-serve-app", "test-cluster")
			if err != nil {
				t.Fatalf("GetActiveDeployment: %v", err)
			}
			if got := active.Deployment().Status(); got != "active" {
				t.Fatalf("GetActiveDeployment rendered served row %q, want %q", got, "active")
			}

			list, err := client.ListDeployments(ctx, "legacy-serve-app", "test-cluster", "", 20)
			if err != nil {
				t.Fatalf("ListDeployments: %v", err)
			}
			deps := list.Deployments()
			if len(deps) != 1 {
				t.Fatalf("ListDeployments returned %d deployments, want 1", len(deps))
			}
			if got := deps[0].Status(); got != "active" {
				t.Errorf("ListDeployments rendered the served legacy active row as %q, want %q", got, "active")
			}

			got, err := client.GetDeploymentById(ctx, servedID.String())
			if err != nil {
				t.Fatalf("GetDeploymentById: %v", err)
			}
			if gotStatus := got.Deployment().Status(); gotStatus != "active" {
				t.Errorf("GetDeploymentById rendered the served legacy active row as %q, want %q", gotStatus, "active")
			}
			if got.Deployment().Id() != string(servedID) {
				t.Errorf("GetDeploymentById returned %q, want %q", got.Deployment().Id(), servedID)
			}
		})
	}
}

// TestUnmigratedLegacyTwoActiveRowsDeclinedByFallback locks in the fix's
// safety choice. When app.active_deployment is empty and two legacy "active"
// rows remain, ListDeployments and GetDeploymentById decline to pick one (both
// render "succeeded") rather than guessing which is serving — the same refusal
// the migration's migrateApp makes. GetActiveDeployment is intentionally left
// to serve the newest active row, which is its existing, unmodified behavior.
func TestUnmigratedLegacyTwoActiveRowsDeclinedByFallback(t *testing.T) {
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

	olderID, err := inmem.Client.Create(ctx, "two-active-older", &core_v1alpha.Deployment{
		AppName:    "two-active-app",
		ClusterId:  "test-cluster",
		AppVersion: "two-active-v1",
		Status:     "active",
		DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-2 * time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("create older active deployment: %v", err)
	}
	newerID, err := inmem.Client.Create(ctx, "two-active-newer", &core_v1alpha.Deployment{
		AppName:    "two-active-app",
		ClusterId:  "test-cluster",
		AppVersion: "two-active-v2",
		Status:     "active",
		DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("create newer active deployment: %v", err)
	}
	if _, err := inmem.Client.Create(ctx, "two-active-app", &core_v1alpha.App{}); err != nil {
		t.Fatalf("create app: %v", err)
	}

	list, err := client.ListDeployments(ctx, "two-active-app", "test-cluster", "", 20)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	deps := list.Deployments()
	if len(deps) != 2 {
		t.Fatalf("ListDeployments returned %d deployments, want 2", len(deps))
	}
	for _, dep := range deps {
		if got := dep.Status(); got != "succeeded" {
			t.Errorf("ListDeployments rendered ambiguous active %q as %q, want %q (refused to disambiguate)",
				dep.Id(), got, "succeeded")
		}
	}

	for _, id := range []string{olderID.String(), newerID.String()} {
		got, err := client.GetDeploymentById(ctx, id)
		if err != nil {
			t.Fatalf("GetDeploymentById(%s): %v", id, err)
		}
		if status := got.Deployment().Status(); status != "succeeded" {
			t.Errorf("GetDeploymentById(%s) rendered ambiguous active as %q, want %q", id, status, "succeeded")
		}
	}

	// GetActiveDeployment is unchanged: it still serves one active row (the
	// newest) and reports it "active". This documents the intentional
	// asymmetry rather than asserting a regression.
	active, err := client.GetActiveDeployment(ctx, "two-active-app", "test-cluster")
	if err != nil {
		t.Fatalf("GetActiveDeployment: %v", err)
	}
	if got := active.Deployment().Status(); got != "active" {
		t.Errorf("GetActiveDeployment rendered ambiguous active %q, want %q", got, "active")
	}
}

// TestListDeploymentsMixedPointerAndLegacyServing exercises the per-app
// serving resolution in a single ListDeployments call that spans both
// migration states at once: one app whose app.active_deployment pointer has
// been backfilled, and a second unmigrated app still serving its single
// legacy "active" row. The fix must resolve each app's serving row
// independently — the pointer-set app keeps rendering its pointer-named row
// active and its older rows succeeded, while the unmigrated app's lone
// active row renders active via the legacy fallback.
func TestListDeploymentsMixedPointerAndLegacyServing(t *testing.T) {
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

	// pointer-app: backfilled app.active_deployment pointing at its newer row.
	olderA, err := inmem.Client.Create(ctx, "pointer-older", &core_v1alpha.Deployment{
		AppName:    "pointer-app",
		ClusterId:  "test-cluster",
		AppVersion: "pointer-v1",
		Status:     "active",
		DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-2 * time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("create older pointer-app deployment: %v", err)
	}
	currentA, err := inmem.Client.Create(ctx, "pointer-current", &core_v1alpha.Deployment{
		AppName:    "pointer-app",
		ClusterId:  "test-cluster",
		AppVersion: "pointer-v2",
		Status:     "active",
		DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("create current pointer-app deployment: %v", err)
	}
	if _, err := inmem.Client.Create(ctx, "pointer-app", &core_v1alpha.App{
		ActiveDeployment: currentA,
	}); err != nil {
		t.Fatalf("create pointer-app: %v", err)
	}

	// legacy-app: unmigrated — empty pointer, single retained legacy active row.
	legacyB, err := inmem.Client.Create(ctx, "legacy-single", &core_v1alpha.Deployment{
		AppName:    "legacy-app",
		ClusterId:  "test-cluster",
		AppVersion: "legacy-v1",
		Status:     "active",
		DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-30 * time.Minute).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("create legacy-app deployment: %v", err)
	}
	if _, err := inmem.Client.Create(ctx, "legacy-app", &core_v1alpha.App{}); err != nil {
		t.Fatalf("create legacy-app: %v", err)
	}

	// No app filter: ListDeployments must resolve serving per app across both.
	list, err := client.ListDeployments(ctx, "", "test-cluster", "", 100)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	want := map[string]string{
		olderA.String():   "succeeded",
		currentA.String(): "active",
		legacyB.String():  "active",
	}
	got := map[string]string{}
	for _, dep := range list.Deployments() {
		got[dep.Id()] = dep.Status()
	}
	if len(got) != len(want) {
		t.Fatalf("ListDeployments returned %d deployments, want %d", len(got), len(want))
	}
	for id, wantStatus := range want {
		if gotStatus, ok := got[id]; !ok {
			t.Errorf("ListDeployments missing deployment %q", id)
		} else if gotStatus != wantStatus {
			t.Errorf("deployment %q status = %q, want %q", id, gotStatus, wantStatus)
		}
	}
}
