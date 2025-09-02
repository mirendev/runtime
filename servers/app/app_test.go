package app

import (
	"context"
	"log/slog"
	"testing"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestSetConfiguration_DuplicateEnvVars(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	// Create AppInfo instance
	appInfo := &AppInfo{
		Log:  slog.Default(),
		EC:   ec,
		CPU:  &metrics.CPUUsage{},
		Mem:  &metrics.MemoryUsage{},
		HTTP: &metrics.HTTPMetrics{},
	}

	// Create RPC client using LocalClient
	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	// Create a test app
	appName := "test-app"
	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, appName, app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.ID = appID

	// Test case 1: Set initial env vars
	t.Run("InitialEnvVars", func(t *testing.T) {
		// Build configuration with new setter methods
		env1 := &app_v1alpha.NamedValue{}
		env1.SetKey("FOO")
		env1.SetValue("bar")
		env1.SetSensitive(false)

		env2 := &app_v1alpha.NamedValue{}
		env2.SetKey("SECRET")
		env2.SetValue("hidden")
		env2.SetSensitive(true)

		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{env1, env2})

		// Use the client to set configuration
		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set configuration: %v", err)
		}

		// Verify the configuration by checking the entity directly
		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		if len(appVerCheck.Config.Variable) != 2 {
			t.Errorf("expected 2 env vars, got %d", len(appVerCheck.Config.Variable))
		}
	})

	// Test case 2: Add duplicate with different sensitive flag
	t.Run("DuplicateWithDifferentSensitiveFlag", func(t *testing.T) {
		// Add FOO again but as sensitive - should replace the existing FOO
		env1 := &app_v1alpha.NamedValue{}
		env1.SetKey("FOO")
		env1.SetValue("secret-bar")
		env1.SetSensitive(true)

		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{env1})

		// Use the client to set configuration
		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set configuration: %v", err)
		}

		// Get configuration and check for duplicates
		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		envVars := appVerCheck.Config.Variable

		// Count FOO occurrences
		fooCount := 0
		for _, ev := range envVars {
			if ev.Key == "FOO" {
				fooCount++
				t.Logf("Found FOO: value=%s, sensitive=%v", ev.Value, ev.Sensitive)
			}
		}

		// With the fix, we should only have 1 FOO (the updated one)
		if fooCount != 1 {
			t.Errorf("Found %d instances of FOO env var (expected 1)", fooCount)
			t.Logf("Total env vars: %d", len(envVars))
			for i, ev := range envVars {
				t.Logf("  [%d] %s = %s (sensitive: %v)", i, ev.Key, ev.Value, ev.Sensitive)
			}
		}
	})

	// Test case 3: Add duplicate with same sensitive flag but different value
	t.Run("DuplicateWithSameSensitiveFlag", func(t *testing.T) {
		// Add SECRET again with different value but same sensitive flag
		env1 := &app_v1alpha.NamedValue{}
		env1.SetKey("SECRET")
		env1.SetValue("updated-secret")
		env1.SetSensitive(true)

		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{env1})

		// Use the client to set configuration
		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set configuration: %v", err)
		}

		// Get configuration and check for duplicates
		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		envVars := appVerCheck.Config.Variable

		// Count SECRET occurrences
		secretCount := 0
		for _, ev := range envVars {
			if ev.Key == "SECRET" {
				secretCount++
				t.Logf("Found SECRET: value=%s, sensitive=%v", ev.Value, ev.Sensitive)
			}
		}

		// With the fix for same key+sensitive, this should work correctly
		if secretCount > 1 {
			t.Errorf("Found %d instances of SECRET env var (expected 1)", secretCount)
		}
	})
}

func TestSetConfiguration_EnvVarDeletion(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	// Create AppInfo instance
	appInfo := &AppInfo{
		Log:  slog.Default(),
		EC:   ec,
		CPU:  &metrics.CPUUsage{},
		Mem:  &metrics.MemoryUsage{},
		HTTP: &metrics.HTTPMetrics{},
	}

	// Create RPC client using LocalClient
	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	// Create a test app
	appName := "test-app-delete"
	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, appName, app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.ID = appID

	// Step 1: Set initial env vars
	t.Run("SetInitialVars", func(t *testing.T) {
		env1 := &app_v1alpha.NamedValue{}
		env1.SetKey("VAR1")
		env1.SetValue("value1")
		env1.SetSensitive(false)

		env2 := &app_v1alpha.NamedValue{}
		env2.SetKey("VAR2")
		env2.SetValue("value2")
		env2.SetSensitive(false)

		env3 := &app_v1alpha.NamedValue{}
		env3.SetKey("VAR3")
		env3.SetValue("value3")
		env3.SetSensitive(true)

		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{env1, env2, env3})

		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set initial configuration: %v", err)
		}

		// Verify all 3 vars are set
		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		if len(appVerCheck.Config.Variable) != 3 {
			t.Errorf("expected 3 env vars, got %d", len(appVerCheck.Config.Variable))
		}
	})

	// Step 2: Delete one var (VAR2)
	t.Run("DeleteOneVar", func(t *testing.T) {
		// Send only VAR1 and VAR3, effectively deleting VAR2
		env1 := &app_v1alpha.NamedValue{}
		env1.SetKey("VAR1")
		env1.SetValue("value1")
		env1.SetSensitive(false)

		env3 := &app_v1alpha.NamedValue{}
		env3.SetKey("VAR3")
		env3.SetValue("value3")
		env3.SetSensitive(true)

		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{env1, env3})

		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set configuration after deletion: %v", err)
		}

		// Verify VAR2 is gone
		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		if len(appVerCheck.Config.Variable) != 2 {
			t.Errorf("expected 2 env vars after deletion, got %d", len(appVerCheck.Config.Variable))
		}

		// Check that VAR2 is specifically gone
		for _, ev := range appVerCheck.Config.Variable {
			if ev.Key == "VAR2" {
				t.Errorf("VAR2 should have been deleted but still exists")
			}
		}

		// Check that VAR1 and VAR3 still exist
		hasVar1, hasVar3 := false, false
		for _, ev := range appVerCheck.Config.Variable {
			if ev.Key == "VAR1" && ev.Value == "value1" {
				hasVar1 = true
			}
			if ev.Key == "VAR3" && ev.Value == "value3" {
				hasVar3 = true
			}
		}
		if !hasVar1 {
			t.Error("VAR1 should still exist after deletion of VAR2")
		}
		if !hasVar3 {
			t.Error("VAR3 should still exist after deletion of VAR2")
		}
	})

	// Step 3: Delete all vars
	t.Run("DeleteAllVars", func(t *testing.T) {
		// Send empty env var list
		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{})

		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set configuration with empty vars: %v", err)
		}

		// Verify all vars are gone
		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		if len(appVerCheck.Config.Variable) != 0 {
			t.Errorf("expected 0 env vars after deleting all, got %d", len(appVerCheck.Config.Variable))
			for _, ev := range appVerCheck.Config.Variable {
				t.Errorf("  unexpected var: %s = %s", ev.Key, ev.Value)
			}
		}
	})
}
