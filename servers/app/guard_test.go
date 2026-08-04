package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// The config-write handlers gained rpc.AllowApp guards so they can be granted to
// an app-scoped workload role without letting a workload reach another app. This
// verifies each guard is actually wired: an identity bound to "other-app" must
// be refused when it targets "victim". An unscoped caller (no identity, as the
// other tests in this package run) is unaffected — that is the behavior-
// preserving half, already covered by the existing handler tests.
func TestCrudWrites_AppScopingGuards(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	appInfo := &AppInfo{
		Log:  slog.Default(),
		EC:   ec,
		CPU:  &metrics.CPUUsage{},
		Mem:  &metrics.MemoryUsage{},
		HTTP: &metrics.HTTPMetrics{},
	}

	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	const victim = "victim"
	if _, err := inmem.Client.Create(ctx, victim, &core_v1alpha.App{}); err != nil {
		t.Fatalf("create app: %v", err)
	}

	// A workload identity confined to a different app.
	otherApp := rpc.ContextWithIdentity(ctx, &rpc.Identity{
		Subject:  "org:o:app:other-app:sandbox:sb-1",
		Method:   rpc.AuthMethodWorkload,
		Metadata: map[string]any{"app": "other-app"},
	})

	vars := []*app_v1alpha.NamedValue{namedValue("K", "v", false)}

	writes := map[string]func(context.Context) error{
		"SetConfiguration": func(c context.Context) error {
			_, err := client.SetConfiguration(c, victim, &app_v1alpha.Configuration{})
			return err
		},
		"SetHost": func(c context.Context) error {
			_, err := client.SetHost(c, victim, "victim.example.com")
			return err
		},
		"SetEnvVar": func(c context.Context) error {
			_, err := client.SetEnvVar(c, victim, "K", "v", false, "")
			return err
		},
		"SetEnvVars": func(c context.Context) error {
			_, err := client.SetEnvVars(c, victim, vars, "")
			return err
		},
		"SetInitialEnvVars": func(c context.Context) error {
			_, err := client.SetInitialEnvVars(c, victim, vars, "")
			return err
		},
		"DeleteEnvVar": func(c context.Context) error {
			_, err := client.DeleteEnvVar(c, victim, "K", "")
			return err
		},
		"Restart": func(c context.Context) error {
			_, err := client.Restart(c, victim, "")
			return err
		},
	}

	for name, call := range writes {
		t.Run(name+"/denied_for_other_app", func(t *testing.T) {
			err := call(otherApp)
			if !errors.Is(err, rpc.ErrUnauthorized) {
				t.Fatalf("expected ErrUnauthorized targeting another app, got %v", err)
			}
		})
	}

	// An identity bound to the victim's own app clears the guard (it may still
	// fail later on missing input, but never with an authorization error).
	ownApp := rpc.ContextWithIdentity(ctx, &rpc.Identity{
		Subject:  "org:o:app:victim:sandbox:sb-1",
		Method:   rpc.AuthMethodWorkload,
		Metadata: map[string]any{"app": victim},
	})

	for name, call := range writes {
		t.Run(name+"/allowed_for_own_app", func(t *testing.T) {
			if err := call(ownApp); errors.Is(err, rpc.ErrUnauthorized) {
				t.Fatalf("own-app caller was blocked by the guard: %v", err)
			}
		})
	}
}

// SetWorkloadRole validates the role and writes it to the app entity. It is the
// operator path (no rpc.AllowApp guard, absent from every token role map), so
// there is no per-app scoping to test here — only validation and persistence.
func TestSetWorkloadRole(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)
	appInfo := &AppInfo{Log: slog.Default(), EC: ec, CPU: &metrics.CPUUsage{}, Mem: &metrics.MemoryUsage{}, HTTP: &metrics.HTTPMetrics{}}
	client := &app_v1alpha.CrudClient{Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo))}

	const appName = "myapp"
	if _, err := inmem.Client.Create(ctx, appName, &core_v1alpha.App{}); err != nil {
		t.Fatalf("create app: %v", err)
	}

	t.Run("rejects an unknown role", func(t *testing.T) {
		if _, err := client.SetWorkloadRole(ctx, appName, "wizard"); err == nil {
			t.Fatal("expected an error for an unknown role")
		}
	})

	t.Run("persists a valid role", func(t *testing.T) {
		if _, err := client.SetWorkloadRole(ctx, appName, "cluster-readonly"); err != nil {
			t.Fatalf("set role: %v", err)
		}

		var app core_v1alpha.App
		if err := ec.Get(ctx, appName, &app); err != nil {
			t.Fatalf("get app: %v", err)
		}
		if app.WorkloadRole != "cluster-readonly" {
			t.Errorf("WorkloadRole = %q, want cluster-readonly", app.WorkloadRole)
		}
	})

	// SetWorkloadRole must leave the rest of the App record intact — it patches
	// only workload_role rather than re-writing the whole entity. This asserts
	// the targeted write directly; the concurrency benefit follows from it (a
	// full re-write would revert a field a concurrent deploy set between our read
	// and write, which a single-attribute patch never touches — the store's own
	// Patch semantics are covered by TestStoreConformance_PatchEntity).
	t.Run("preserves sibling fields", func(t *testing.T) {
		const other = "sibling-app"
		if _, err := inmem.Client.Create(ctx, other, &core_v1alpha.App{
			ActiveVersion: "version/keep-me",
			Project:       "project/team",
		}); err != nil {
			t.Fatalf("create app: %v", err)
		}

		if _, err := client.SetWorkloadRole(ctx, other, "app-admin"); err != nil {
			t.Fatalf("set role: %v", err)
		}

		var app core_v1alpha.App
		if err := ec.Get(ctx, other, &app); err != nil {
			t.Fatalf("get app: %v", err)
		}
		if app.WorkloadRole != "app-admin" {
			t.Errorf("WorkloadRole = %q, want app-admin", app.WorkloadRole)
		}
		if app.ActiveVersion != "version/keep-me" {
			t.Errorf("ActiveVersion = %q, want version/keep-me (clobbered by the role write)", app.ActiveVersion)
		}
		if app.Project != "project/team" {
			t.Errorf("Project = %q, want project/team (clobbered by the role write)", app.Project)
		}
	})
}

func namedValue(key, value string, sensitive bool) *app_v1alpha.NamedValue {
	nv := &app_v1alpha.NamedValue{}
	nv.SetKey(key)
	nv.SetValue(value)
	nv.SetSensitive(sensitive)
	return nv
}
