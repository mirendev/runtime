package app

import (
	"context"
	"log/slog"
	"testing"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/entity"
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

func TestSetConfiguration_CleanupExistingDuplicates(t *testing.T) {
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

	// Create a test app using the raw client
	appName := "test-app-duplicates"
	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, appName, app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.ID = appID

	// Simulate an entity with existing duplicates by directly creating an AppVersion
	// with duplicate environment variables (as would exist from the old buggy code)
	t.Run("SimulateExistingDuplicates", func(t *testing.T) {
		appVer := &core_v1alpha.AppVersion{
			ID:      entity.Id(""), // Will be set by Create
			App:     appID,
			Version: appName + "-v1",
			Config: core_v1alpha.Config{
				// Directly set duplicate variables to simulate the buggy state
				// The bug would have appended duplicates, so we simulate that here
				Variable: []core_v1alpha.Variable{
					{Key: "FOO", Value: "first", Sensitive: false},
					{Key: "BAR", Value: "baz", Sensitive: false},
					{Key: "FOO", Value: "second", Sensitive: false}, // Duplicate!
					{Key: "SECRET", Value: "hidden", Sensitive: true},
					{Key: "FOO", Value: "third", Sensitive: true},      // Another duplicate!
					{Key: "SECRET", Value: "updated", Sensitive: true}, // Duplicate!
				},
			},
		}

		// Create the version with duplicates using raw client
		versionID, err := inmem.Client.Create(ctx, appVer.Version, appVer)
		if err != nil {
			t.Fatalf("failed to create app version: %v", err)
		}
		appVer.ID = versionID

		// Update the app to point to this version
		app.ActiveVersion = versionID
		err = inmem.Client.Update(ctx, app)
		if err != nil {
			t.Fatalf("failed to update app: %v", err)
		}

		// Verify duplicates exist by checking entity directly
		var appCheck core_v1alpha.App
		err = inmem.Client.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVerCheck core_v1alpha.AppVersion
		err = inmem.Client.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		envVars := appVerCheck.Config.Variable
		t.Logf("Initial state with duplicates: %d total env vars", len(envVars))
		for i, ev := range envVars {
			t.Logf("  [%d] %s = %s (sensitive: %v)", i, ev.Key, ev.Value, ev.Sensitive)
		}

		// We should have 6 total variables (including duplicates)
		if len(envVars) != 6 {
			t.Errorf("expected 6 env vars with duplicates, got %d", len(envVars))
		}
	})

	// Now test that SetConfiguration cleans up the duplicates
	// The last value should win for each key
	t.Run("CleanupDuplicatesLastValueWins", func(t *testing.T) {
		// Add a new env var to trigger SetConfiguration
		env1 := &app_v1alpha.NamedValue{}
		env1.SetKey("NEW_VAR")
		env1.SetValue("new_value")
		env1.SetSensitive(false)

		cfg := &app_v1alpha.Configuration{}
		cfg.SetEnvVars([]*app_v1alpha.NamedValue{env1})

		// Use the client to set configuration
		_, err := client.SetConfiguration(ctx, appName, cfg)
		if err != nil {
			t.Fatalf("failed to set configuration: %v", err)
		}

		// Get configuration and check that duplicates are cleaned up
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
		t.Logf("After cleanup: %d total env vars", len(envVars))
		for i, ev := range envVars {
			t.Logf("  [%d] %s = %s (sensitive: %v)", i, ev.Key, ev.Value, ev.Sensitive)
		}

		// Check that duplicates are removed
		// We should have: FOO (last value: "third", sensitive: true),
		// BAR ("baz"), SECRET (last value: "updated"), NEW_VAR ("new_value")
		if len(envVars) != 4 {
			t.Errorf("expected 4 env vars after cleanup, got %d", len(envVars))
		}

		// Verify the last values won
		expectedVars := map[string]struct {
			value     string
			sensitive bool
		}{
			"FOO":     {"third", true}, // Last duplicate was sensitive
			"BAR":     {"baz", false},
			"SECRET":  {"updated", true}, // Last duplicate value
			"NEW_VAR": {"new_value", false},
		}

		for _, ev := range envVars {
			expected, ok := expectedVars[ev.Key]
			if !ok {
				t.Errorf("unexpected env var: %s", ev.Key)
				continue
			}

			if ev.Value != expected.value {
				t.Errorf("%s: expected value '%s', got '%s'", ev.Key, expected.value, ev.Value)
			}

			if ev.Sensitive != expected.sensitive {
				t.Errorf("%s: expected sensitive=%v, got %v", ev.Key, expected.sensitive, ev.Sensitive)
			}

			delete(expectedVars, ev.Key)
		}

		// Check if any expected vars are missing
		for key := range expectedVars {
			t.Errorf("missing expected env var: %s", key)
		}
	})
}
