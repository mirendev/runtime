package app

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
)

func TestDeployTrackerInitializesOnceConcurrently(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	appInfo := &AppInfo{
		Log: slog.Default(),
		EC:  entityserver.NewClient(slog.Default(), inmem.EAC),
	}

	const callers = 20
	start := make(chan struct{})
	results := make(chan *deploylifecycle.Tracker, callers)
	var workers sync.WaitGroup
	workers.Add(callers)
	for range callers {
		go func() {
			defer workers.Done()
			<-start
			results <- appInfo.deployTracker()
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	var first *deploylifecycle.Tracker
	for tracker := range results {
		if first == nil {
			first = tracker
		}
		assert.Same(t, first, tracker)
	}
	require.NotNil(t, first)
}

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

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Variables) != 2 {
			t.Errorf("expected 2 env vars, got %d", len(resolvedCfg.Variables))
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

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		envVars := resolvedCfg.Variables

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

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		envVars := resolvedCfg.Variables

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

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Variables) != 3 {
			t.Errorf("expected 3 env vars, got %d", len(resolvedCfg.Variables))
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

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Variables) != 2 {
			t.Errorf("expected 2 env vars after deletion, got %d", len(resolvedCfg.Variables))
		}

		// Check that VAR2 is specifically gone
		for _, ev := range resolvedCfg.Variables {
			if ev.Key == "VAR2" {
				t.Errorf("VAR2 should have been deleted but still exists")
			}
		}

		// Check that VAR1 and VAR3 still exist
		hasVar1, hasVar3 := false, false
		for _, ev := range resolvedCfg.Variables {
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

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Variables) != 0 {
			t.Errorf("expected 0 env vars after deleting all, got %d", len(resolvedCfg.Variables))
			for _, ev := range resolvedCfg.Variables {
				t.Errorf("  unexpected var: %s = %s", ev.Key, ev.Value)
			}
		}
	})
}

func TestSetEnvVars_Batch(t *testing.T) {
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

	t.Run("SetMultipleVarsAtOnce", func(t *testing.T) {
		appName := "test-batch-set"
		app := &core_v1alpha.App{}
		appID, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}
		app.ID = appID

		nv1 := &app_v1alpha.NamedValue{}
		nv1.SetKey("A")
		nv1.SetValue("1")
		nv1.SetSensitive(false)

		nv2 := &app_v1alpha.NamedValue{}
		nv2.SetKey("B")
		nv2.SetValue("2")
		nv2.SetSensitive(false)

		nv3 := &app_v1alpha.NamedValue{}
		nv3.SetKey("C")
		nv3.SetValue("3")
		nv3.SetSensitive(true)

		res, err := client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv1, nv2, nv3}, "")
		if err != nil {
			t.Fatalf("SetEnvVars failed: %v", err)
		}

		if res.VersionId() == "" {
			t.Fatal("expected non-empty version ID")
		}

		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVer core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVer)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVer)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Variables) != 3 {
			t.Fatalf("expected 3 env vars, got %d", len(resolvedCfg.Variables))
		}

		vars := map[string]core_v1alpha.ConfigSpecVariables{}
		for _, v := range resolvedCfg.Variables {
			vars[v.Key] = v
		}

		if vars["A"].Value != "1" || vars["A"].Sensitive {
			t.Errorf("A: got value=%q sensitive=%v, want value=%q sensitive=%v", vars["A"].Value, vars["A"].Sensitive, "1", false)
		}
		if vars["B"].Value != "2" || vars["B"].Sensitive {
			t.Errorf("B: got value=%q sensitive=%v, want value=%q sensitive=%v", vars["B"].Value, vars["B"].Sensitive, "2", false)
		}
		if vars["C"].Value != "3" || !vars["C"].Sensitive {
			t.Errorf("C: got value=%q sensitive=%v, want value=%q sensitive=%v", vars["C"].Value, vars["C"].Sensitive, "3", true)
		}
		for _, v := range resolvedCfg.Variables {
			if v.Source != "manual" {
				t.Errorf("var %s: expected source %q, got %q", v.Key, "manual", v.Source)
			}
		}
	})

	t.Run("CreatesOnlyOneVersion", func(t *testing.T) {
		appName := "test-batch-single-version"
		app := &core_v1alpha.App{}
		appID, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}
		app.ID = appID

		nv1 := &app_v1alpha.NamedValue{}
		nv1.SetKey("X")
		nv1.SetValue("10")
		nv1.SetSensitive(false)

		nv2 := &app_v1alpha.NamedValue{}
		nv2.SetKey("Y")
		nv2.SetValue("20")
		nv2.SetSensitive(false)

		res, err := client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv1, nv2}, "")
		if err != nil {
			t.Fatalf("SetEnvVars failed: %v", err)
		}
		firstVersionId := res.VersionId()

		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVer core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVer)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		if appVer.Version != firstVersionId {
			t.Errorf("active version %q does not match returned version %q", appVer.Version, firstVersionId)
		}
	})

	t.Run("UpdatesExistingVars", func(t *testing.T) {
		appName := "test-batch-update"
		app := &core_v1alpha.App{}
		appID, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}
		app.ID = appID

		// Set initial vars
		nv1 := &app_v1alpha.NamedValue{}
		nv1.SetKey("KEY1")
		nv1.SetValue("old1")
		nv1.SetSensitive(false)

		nv2 := &app_v1alpha.NamedValue{}
		nv2.SetKey("KEY2")
		nv2.SetValue("old2")
		nv2.SetSensitive(false)

		_, err = client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv1, nv2}, "")
		if err != nil {
			t.Fatalf("initial SetEnvVars failed: %v", err)
		}

		// Update KEY1 and add KEY3 in a single batch
		upd1 := &app_v1alpha.NamedValue{}
		upd1.SetKey("KEY1")
		upd1.SetValue("new1")
		upd1.SetSensitive(true)

		upd3 := &app_v1alpha.NamedValue{}
		upd3.SetKey("KEY3")
		upd3.SetValue("val3")
		upd3.SetSensitive(false)

		_, err = client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{upd1, upd3}, "")
		if err != nil {
			t.Fatalf("update SetEnvVars failed: %v", err)
		}

		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var appVer core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &appVer)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVer)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Variables) != 3 {
			t.Fatalf("expected 3 env vars, got %d", len(resolvedCfg.Variables))
		}

		vars := map[string]core_v1alpha.ConfigSpecVariables{}
		for _, v := range resolvedCfg.Variables {
			vars[v.Key] = v
		}

		if vars["KEY1"].Value != "new1" || !vars["KEY1"].Sensitive {
			t.Errorf("KEY1: got value=%q sensitive=%v, want value=%q sensitive=%v", vars["KEY1"].Value, vars["KEY1"].Sensitive, "new1", true)
		}
		if vars["KEY2"].Value != "old2" {
			t.Errorf("KEY2: got value=%q, want %q (should be unchanged)", vars["KEY2"].Value, "old2")
		}
		if vars["KEY3"].Value != "val3" {
			t.Errorf("KEY3: got value=%q, want %q", vars["KEY3"].Value, "val3")
		}
	})

	t.Run("RejectsMIRENPrefix", func(t *testing.T) {
		appName := "test-batch-miren-reject"
		app := &core_v1alpha.App{}
		appID, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}
		app.ID = appID

		nv1 := &app_v1alpha.NamedValue{}
		nv1.SetKey("GOOD_VAR")
		nv1.SetValue("ok")
		nv1.SetSensitive(false)

		nv2 := &app_v1alpha.NamedValue{}
		nv2.SetKey("MIREN_SECRET")
		nv2.SetValue("bad")
		nv2.SetSensitive(false)

		_, err = client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv1, nv2}, "")
		if err == nil {
			t.Fatal("expected error for MIREN_ prefix, got nil")
		}
	})

	t.Run("PerServiceEnvVars", func(t *testing.T) {
		appName := "test-batch-service"
		app := &core_v1alpha.App{}
		appID, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}
		app.ID = appID

		// Create an initial version with a service defined
		appVer := core_v1alpha.AppVersion{
			App: appID,
			Config: core_v1alpha.Config{
				Services: []core_v1alpha.Services{
					{Name: "web"},
				},
			},
		}
		appVer.Version = appName + "-v0"
		avid, err := inmem.Client.Create(ctx, appVer.Version, &appVer)
		if err != nil {
			t.Fatalf("failed to create initial version: %v", err)
		}

		var appRec core_v1alpha.App
		err = ec.Get(ctx, appName, &appRec)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}
		appRec.ActiveVersion = avid
		err = ec.Update(ctx, &appRec)
		if err != nil {
			t.Fatalf("failed to update app: %v", err)
		}

		nv1 := &app_v1alpha.NamedValue{}
		nv1.SetKey("DB_HOST")
		nv1.SetValue("localhost")
		nv1.SetSensitive(false)

		nv2 := &app_v1alpha.NamedValue{}
		nv2.SetKey("DB_PASS")
		nv2.SetValue("secret")
		nv2.SetSensitive(true)

		_, err = client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv1, nv2}, "web")
		if err != nil {
			t.Fatalf("SetEnvVars for service failed: %v", err)
		}

		var appCheck core_v1alpha.App
		err = ec.Get(ctx, appName, &appCheck)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var ver core_v1alpha.AppVersion
		err = ec.GetById(ctx, appCheck.ActiveVersion, &ver)
		if err != nil {
			t.Fatalf("failed to get app version: %v", err)
		}

		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &ver)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}

		if len(resolvedCfg.Services) != 1 {
			t.Fatalf("expected 1 service, got %d", len(resolvedCfg.Services))
		}

		svc := resolvedCfg.Services[0]
		if len(svc.Env) != 2 {
			t.Fatalf("expected 2 service env vars, got %d", len(svc.Env))
		}

		envMap := map[string]core_v1alpha.ConfigSpecServicesEnv{}
		for _, e := range svc.Env {
			envMap[e.Key] = e
		}

		if envMap["DB_HOST"].Value != "localhost" || envMap["DB_HOST"].Sensitive {
			t.Errorf("DB_HOST: got value=%q sensitive=%v, want value=%q sensitive=%v", envMap["DB_HOST"].Value, envMap["DB_HOST"].Sensitive, "localhost", false)
		}
		if envMap["DB_PASS"].Value != "secret" || !envMap["DB_PASS"].Sensitive {
			t.Errorf("DB_PASS: got value=%q sensitive=%v, want value=%q sensitive=%v", envMap["DB_PASS"].Value, envMap["DB_PASS"].Sensitive, "secret", true)
		}

		if len(resolvedCfg.Variables) != 0 {
			t.Errorf("expected 0 global env vars, got %d", len(resolvedCfg.Variables))
		}
	})

	t.Run("ServiceCreatedOnFirstUse", func(t *testing.T) {
		// Setting a service-scoped env var on a service that doesn't yet
		// have a config entry should create the entry rather than error.
		// Service entries are otherwise only populated by the build step,
		// so we'd reject every first-time scoped set.
		appName := "test-batch-newsvc"
		app := &core_v1alpha.App{}
		appID, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}
		app.ID = appID

		appVer := core_v1alpha.AppVersion{App: appID}
		appVer.Version = appName + "-v0"
		avid, err := inmem.Client.Create(ctx, appVer.Version, &appVer)
		if err != nil {
			t.Fatalf("failed to create initial version: %v", err)
		}

		var appRec core_v1alpha.App
		err = ec.Get(ctx, appName, &appRec)
		if err != nil {
			t.Fatalf("failed to get app: %v", err)
		}
		appRec.ActiveVersion = avid
		err = ec.Update(ctx, &appRec)
		if err != nil {
			t.Fatalf("failed to update app: %v", err)
		}

		nv := &app_v1alpha.NamedValue{}
		nv.SetKey("X")
		nv.SetValue("1")
		nv.SetSensitive(false)

		_, err = client.SetEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv}, "worker")
		if err != nil {
			t.Fatalf("expected service entry to be created, got error: %v", err)
		}

		err = ec.Get(ctx, appName, &appRec)
		if err != nil {
			t.Fatalf("failed to reload app: %v", err)
		}

		var newVer core_v1alpha.AppVersion
		if err := ec.GetById(ctx, appRec.ActiveVersion, &newVer); err != nil {
			t.Fatalf("failed to get active version: %v", err)
		}
		resolved, err := coreutil.ResolveConfig(ctx, ec.EAC(), &newVer)
		if err != nil {
			t.Fatalf("failed to resolve config: %v", err)
		}
		var foundEnv *core_v1alpha.ConfigSpecServicesEnv
		for i := range resolved.Services {
			if resolved.Services[i].Name == "worker" {
				for j := range resolved.Services[i].Env {
					if resolved.Services[i].Env[j].Key == "X" {
						foundEnv = &resolved.Services[i].Env[j]
					}
				}
			}
		}
		if foundEnv == nil {
			t.Fatal("expected worker service entry with X=1, not found")
			return
		}
		if foundEnv.Value != "1" {
			t.Fatalf("expected X=1, got %q", foundEnv.Value)
		}
	})
}

// setupAppWithConfig creates a test app with the given global vars and services via SetConfiguration.
// Returns the app name and the CrudClient.
func setupAppWithConfig(
	t *testing.T,
	ctx context.Context,
	ec *entityserver.Client,
	appInfo *AppInfo,
	appName string,
	globalVars []*app_v1alpha.NamedValue,
	commands []*app_v1alpha.ServiceCommand,
	services []*app_v1alpha.ServiceConfig,
) *app_v1alpha.CrudClient {
	t.Helper()

	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	app := &core_v1alpha.App{}
	appID, err := ec.Create(ctx, appName, app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	_ = appID

	cfg := &app_v1alpha.Configuration{}
	if globalVars != nil {
		cfg.SetEnvVars(globalVars)
	}
	if commands != nil {
		cfg.SetCommands(commands)
	}
	if services != nil {
		cfg.SetServices(services)
	}

	_, err = client.SetConfiguration(ctx, appName, cfg)
	if err != nil {
		t.Fatalf("failed to set initial configuration: %v", err)
	}

	return client
}

func resolveAppConfig(t *testing.T, ctx context.Context, ec *entityserver.Client, appName string) *core_v1alpha.ConfigSpec {
	t.Helper()

	var appCheck core_v1alpha.App
	err := ec.Get(ctx, appName, &appCheck)
	if err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	var appVerCheck core_v1alpha.AppVersion
	err = ec.GetById(ctx, appCheck.ActiveVersion, &appVerCheck)
	if err != nil {
		t.Fatalf("failed to get app version: %v", err)
	}

	resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVerCheck)
	if err != nil {
		t.Fatalf("failed to resolve config: %v", err)
	}

	return resolvedCfg
}

func makeNamedValue(key, value string, sensitive bool) *app_v1alpha.NamedValue {
	nv := &app_v1alpha.NamedValue{}
	nv.SetKey(key)
	nv.SetValue(value)
	nv.SetSensitive(sensitive)
	return nv
}

func TestSetEnvVar(t *testing.T) {
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

	t.Run("adds global var preserving existing vars", func(t *testing.T) {
		appName := "test-setenv-add"
		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("FOO", "foo_val", false),
				makeNamedValue("BAR", "bar_val", false),
			},
			nil, nil,
		)

		_, err := client.SetEnvVar(ctx, appName, "BAZ", "baz_val", false, "")
		if err != nil {
			t.Fatalf("SetEnvVar failed: %v", err)
		}

		cfg := resolveAppConfig(t, ctx, ec, appName)
		if len(cfg.Variables) != 3 {
			t.Fatalf("expected 3 vars, got %d", len(cfg.Variables))
		}

		varMap := make(map[string]core_v1alpha.ConfigSpecVariables)
		for _, v := range cfg.Variables {
			varMap[v.Key] = v
		}

		for _, key := range []string{"FOO", "BAR", "BAZ"} {
			if _, ok := varMap[key]; !ok {
				t.Errorf("expected var %s to be present", key)
			}
		}
		if varMap["BAZ"].Source != "manual" {
			t.Errorf("expected BAZ source=manual, got %q", varMap["BAZ"].Source)
		}
	})

	t.Run("updates existing global var", func(t *testing.T) {
		appName := "test-setenv-update"
		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("FOO", "old_val", false),
				makeNamedValue("BAR", "bar_val", false),
			},
			nil, nil,
		)

		_, err := client.SetEnvVar(ctx, appName, "FOO", "new_val", true, "")
		if err != nil {
			t.Fatalf("SetEnvVar failed: %v", err)
		}

		cfg := resolveAppConfig(t, ctx, ec, appName)
		if len(cfg.Variables) != 2 {
			t.Fatalf("expected 2 vars, got %d", len(cfg.Variables))
		}

		varMap := make(map[string]core_v1alpha.ConfigSpecVariables)
		for _, v := range cfg.Variables {
			varMap[v.Key] = v
		}

		if varMap["FOO"].Value != "new_val" {
			t.Errorf("expected FOO value=new_val, got %q", varMap["FOO"].Value)
		}
		if varMap["FOO"].Sensitive != true {
			t.Errorf("expected FOO sensitive=true")
		}
		if varMap["FOO"].Source != "manual" {
			t.Errorf("expected FOO source=manual, got %q", varMap["FOO"].Source)
		}
		if varMap["BAR"].Value != "bar_val" {
			t.Errorf("expected BAR to be unchanged, got %q", varMap["BAR"].Value)
		}
	})

	t.Run("adds per-service var preserving global vars", func(t *testing.T) {
		appName := "test-setenv-svc"

		// Create a service via Commands so it exists in spec.Services
		cmd := &app_v1alpha.ServiceCommand{}
		cmd.SetService("web")
		cmd.SetCommand("./start-web")

		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("GLOBAL1", "g1", false),
			},
			[]*app_v1alpha.ServiceCommand{cmd},
			nil,
		)

		_, err := client.SetEnvVar(ctx, appName, "SVC_KEY", "svc_val", false, "web")
		if err != nil {
			t.Fatalf("SetEnvVar failed: %v", err)
		}

		cfg := resolveAppConfig(t, ctx, ec, appName)

		// Global var preserved
		if len(cfg.Variables) != 1 {
			t.Fatalf("expected 1 global var, got %d", len(cfg.Variables))
		}
		if cfg.Variables[0].Key != "GLOBAL1" || cfg.Variables[0].Value != "g1" {
			t.Errorf("global var changed: %+v", cfg.Variables[0])
		}

		// Service var added
		var webSvc *core_v1alpha.ConfigSpecServices
		for i := range cfg.Services {
			if cfg.Services[i].Name == "web" {
				webSvc = &cfg.Services[i]
				break
			}
		}
		if webSvc == nil {
			t.Fatal("web service not found")
			return
		}
		if len(webSvc.Env) != 1 {
			t.Fatalf("expected 1 service env var, got %d", len(webSvc.Env))
		}
		if webSvc.Env[0].Key != "SVC_KEY" || webSvc.Env[0].Value != "svc_val" {
			t.Errorf("unexpected service env var: %+v", webSvc.Env[0])
		}
		if webSvc.Env[0].Source != "manual" {
			t.Errorf("expected service env source=manual, got %q", webSvc.Env[0].Source)
		}
	})

	t.Run("creates service entry on first scoped set", func(t *testing.T) {
		appName := "test-setenv-newsvc"
		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("FOO", "bar", false),
			},
			nil, nil,
		)

		_, err := client.SetEnvVar(ctx, appName, "KEY", "val", false, "worker")
		if err != nil {
			t.Fatalf("expected service to be created on first scoped set, got: %v", err)
		}
	})
}

func TestDeleteEnvVar(t *testing.T) {
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

	t.Run("deletes global var preserving others", func(t *testing.T) {
		appName := "test-delenv-global"
		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("FOO", "foo_val", false),
				makeNamedValue("BAR", "bar_val", false),
				makeNamedValue("BAZ", "baz_val", true),
			},
			nil, nil,
		)

		_, err := client.DeleteEnvVar(ctx, appName, "BAR", "")
		if err != nil {
			t.Fatalf("DeleteEnvVar failed: %v", err)
		}

		cfg := resolveAppConfig(t, ctx, ec, appName)
		if len(cfg.Variables) != 2 {
			t.Fatalf("expected 2 vars, got %d", len(cfg.Variables))
		}

		varMap := make(map[string]core_v1alpha.ConfigSpecVariables)
		for _, v := range cfg.Variables {
			varMap[v.Key] = v
		}

		if _, ok := varMap["BAR"]; ok {
			t.Error("BAR should have been deleted")
		}
		if varMap["FOO"].Value != "foo_val" {
			t.Errorf("FOO should be preserved, got %q", varMap["FOO"].Value)
		}
		if varMap["BAZ"].Value != "baz_val" {
			t.Errorf("BAZ should be preserved, got %q", varMap["BAZ"].Value)
		}
	})

	t.Run("deletes per-service var preserving others", func(t *testing.T) {
		appName := "test-delenv-svc"

		cmd := &app_v1alpha.ServiceCommand{}
		cmd.SetService("web")
		cmd.SetCommand("./start")

		svcEnv1 := makeNamedValue("SVC_A", "a", false)
		svcEnv2 := makeNamedValue("SVC_B", "b", false)
		svcCfg := &app_v1alpha.ServiceConfig{}
		svcCfg.SetService("web")
		svcCfg.SetServiceEnv([]*app_v1alpha.NamedValue{svcEnv1, svcEnv2})

		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("GLOBAL", "gval", false),
			},
			[]*app_v1alpha.ServiceCommand{cmd},
			[]*app_v1alpha.ServiceConfig{svcCfg},
		)

		_, err := client.DeleteEnvVar(ctx, appName, "SVC_A", "web")
		if err != nil {
			t.Fatalf("DeleteEnvVar failed: %v", err)
		}

		cfg := resolveAppConfig(t, ctx, ec, appName)

		// Global var preserved
		if len(cfg.Variables) != 1 || cfg.Variables[0].Key != "GLOBAL" {
			t.Errorf("global var should be preserved: %+v", cfg.Variables)
		}

		// Service var SVC_B preserved, SVC_A removed
		var webSvc *core_v1alpha.ConfigSpecServices
		for i := range cfg.Services {
			if cfg.Services[i].Name == "web" {
				webSvc = &cfg.Services[i]
				break
			}
		}
		if webSvc == nil {
			t.Fatal("web service not found")
			return
		}
		if len(webSvc.Env) != 1 {
			t.Fatalf("expected 1 service env var, got %d", len(webSvc.Env))
		}
		if webSvc.Env[0].Key != "SVC_B" {
			t.Errorf("expected SVC_B to remain, got %s", webSvc.Env[0].Key)
		}
	})

	t.Run("error on non-existent var", func(t *testing.T) {
		appName := "test-delenv-novar"
		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("FOO", "bar", false),
			},
			nil, nil,
		)

		_, err := client.DeleteEnvVar(ctx, appName, "NONEXISTENT", "")
		if err == nil {
			t.Fatal("expected error for non-existent var")
		}
	})

	t.Run("error on non-existent service", func(t *testing.T) {
		appName := "test-delenv-nosvc"
		client := setupAppWithConfig(t, ctx, ec, appInfo, appName,
			[]*app_v1alpha.NamedValue{
				makeNamedValue("FOO", "bar", false),
			},
			nil, nil,
		)

		_, err := client.DeleteEnvVar(ctx, appName, "KEY", "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent service")
		}
	})
}

func TestSetInitialEnvVars(t *testing.T) {
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

	t.Run("creates ConfigVersion and points app.initial_config without an AppVersion", func(t *testing.T) {
		appName := "test-init-config"
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}

		nv1 := makeNamedValue("SECRET_KEY_BASE", "abc123", true)
		nv2 := makeNamedValue("DATABASE_URL", "postgres://x", true)

		res, err := client.SetInitialEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv1, nv2}, "")
		if err != nil {
			t.Fatalf("SetInitialEnvVars failed: %v", err)
		}
		if res.ConfigVersionId() == "" {
			t.Fatal("expected non-empty config version ID")
		}

		var appCheck core_v1alpha.App
		if err := ec.Get(ctx, appName, &appCheck); err != nil {
			t.Fatalf("failed to get app: %v", err)
		}
		if appCheck.ActiveVersion != "" {
			t.Fatalf("active_version should be empty, got %s", appCheck.ActiveVersion)
		}
		if appCheck.InitialConfig == "" {
			t.Fatal("initial_config should be set")
		}

		var cv core_v1alpha.ConfigVersion
		if err := ec.GetById(ctx, appCheck.InitialConfig, &cv); err != nil {
			t.Fatalf("failed to get initial config version: %v", err)
		}
		if len(cv.Spec.Variables) != 2 {
			t.Fatalf("expected 2 variables in initial config, got %d", len(cv.Spec.Variables))
		}
	})

	t.Run("merges with existing initial config on subsequent calls", func(t *testing.T) {
		appName := "test-init-merge"
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}

		_, err = client.SetInitialEnvVars(ctx, appName,
			[]*app_v1alpha.NamedValue{makeNamedValue("FIRST", "1", false)}, "")
		if err != nil {
			t.Fatalf("first SetInitialEnvVars failed: %v", err)
		}

		_, err = client.SetInitialEnvVars(ctx, appName,
			[]*app_v1alpha.NamedValue{makeNamedValue("SECOND", "2", false)}, "")
		if err != nil {
			t.Fatalf("second SetInitialEnvVars failed: %v", err)
		}

		var appCheck core_v1alpha.App
		if err := ec.Get(ctx, appName, &appCheck); err != nil {
			t.Fatalf("failed to get app: %v", err)
		}

		var cv core_v1alpha.ConfigVersion
		if err := ec.GetById(ctx, appCheck.InitialConfig, &cv); err != nil {
			t.Fatalf("failed to get initial config version: %v", err)
		}

		keys := map[string]bool{}
		for _, v := range cv.Spec.Variables {
			keys[v.Key] = true
		}
		if !keys["FIRST"] || !keys["SECOND"] {
			t.Fatalf("expected merged keys, got %v", keys)
		}
	})

	t.Run("rejects calls after an AppVersion exists", func(t *testing.T) {
		appName := "test-init-after-deploy"
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}

		// Simulate a deploy by creating a normal AppVersion + setting it active.
		_, err = client.SetEnvVars(ctx, appName,
			[]*app_v1alpha.NamedValue{makeNamedValue("DEPLOYED", "yes", false)}, "")
		if err != nil {
			t.Fatalf("SetEnvVars failed: %v", err)
		}

		_, err = client.SetInitialEnvVars(ctx, appName,
			[]*app_v1alpha.NamedValue{makeNamedValue("LATE", "x", false)}, "")
		if err == nil {
			t.Fatal("expected error when setting initial env vars after deploy")
		}
	})

	t.Run("creates service entry when staging service-scoped vars on a fresh app", func(t *testing.T) {
		appName := "test-init-newsvc"
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, appName, app)
		if err != nil {
			t.Fatalf("failed to create app: %v", err)
		}

		nv := makeNamedValue("WORKER_TOKEN", "secret", true)
		_, err = client.SetInitialEnvVars(ctx, appName,
			[]*app_v1alpha.NamedValue{nv}, "worker")
		if err != nil {
			t.Fatalf("expected service entry to be created on fresh app, got: %v", err)
		}

		var appCheck core_v1alpha.App
		if err := ec.Get(ctx, appName, &appCheck); err != nil {
			t.Fatalf("failed to get app: %v", err)
		}
		var cv core_v1alpha.ConfigVersion
		if err := ec.GetById(ctx, appCheck.InitialConfig, &cv); err != nil {
			t.Fatalf("failed to get initial config: %v", err)
		}
		if len(cv.Spec.Services) != 1 || cv.Spec.Services[0].Name != "worker" {
			t.Fatalf("expected single worker service entry, got %+v", cv.Spec.Services)
		}
		if len(cv.Spec.Services[0].Env) != 1 || cv.Spec.Services[0].Env[0].Key != "WORKER_TOKEN" {
			t.Fatalf("expected WORKER_TOKEN in worker service, got %+v", cv.Spec.Services[0].Env)
		}
	})
}

// pinningResolver resolves any reference to itself, which is all this test
// needs: it is about the backend field surviving, not about what it resolves to.
type pinningResolver struct{}

func (pinningResolver) ResolveRef(ctx context.Context, backend, ref string) (secret.SecretValue, error) {
	return secret.SecretValue{Ref: ref, Bytes: []byte("value")}, nil
}

// A variable that loses its backend on the way in becomes an ordinary literal
// holding a reference path, and every guard downstream then reads it as fine:
// nothing pins it, nothing materializes it, and the app receives the path text
// where its credential belongs. Silent by construction, so it is worth pinning
// down at each write path rather than trusting the next caller to notice.
func TestConfigWritePathsPreserveTheSecretBackend(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	appInfo := &AppInfo{
		Log:     slog.Default(),
		EC:      ec,
		CPU:     &metrics.CPUUsage{},
		Mem:     &metrics.MemoryUsage{},
		HTTP:    &metrics.HTTPMetrics{},
		Secrets: pinningResolver{},
	}

	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	appName := "test-backend-passthrough"
	_, err := inmem.Client.Create(ctx, appName, &core_v1alpha.App{})
	require.NoError(t, err)

	reference := func(key string) *app_v1alpha.NamedValue {
		nv := &app_v1alpha.NamedValue{}
		nv.SetKey(key)
		nv.SetValue("payments/stripe-key@x1A")
		nv.SetSensitive(true)
		nv.SetBackend("cluster")
		return nv
	}

	// SetConfiguration rebuilds the variable list wholesale from what the
	// client sent, for both shared and per-service variables.
	svc := &app_v1alpha.ServiceConfig{}
	svc.SetService("worker")
	svc.SetServiceEnv([]*app_v1alpha.NamedValue{reference("WORKER_SECRET")})

	cfg := &app_v1alpha.Configuration{}
	cfg.SetEnvVars([]*app_v1alpha.NamedValue{reference("STRIPE_API_KEY")})
	cfg.SetServices([]*app_v1alpha.ServiceConfig{svc})

	_, err = client.SetConfiguration(ctx, appName, cfg)
	require.NoError(t, err)

	var appRec core_v1alpha.App
	require.NoError(t, ec.Get(ctx, appName, &appRec))

	var appVer core_v1alpha.AppVersion
	require.NoError(t, ec.GetById(ctx, appRec.ActiveVersion, &appVer))

	stored, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVer)
	require.NoError(t, err)

	require.Len(t, stored.Variables, 1)
	assert.Equal(t, "cluster", stored.Variables[0].Backend,
		"the backend must survive SetConfiguration, or the reference becomes a literal")

	require.Len(t, stored.Services, 1)
	require.Len(t, stored.Services[0].Env, 1)
	assert.Equal(t, "cluster", stored.Services[0].Env[0].Backend,
		"the backend must survive the per-service write path too")

	// And it must come back out again, so a round trip through the API does not
	// quietly drop it either.
	read, err := client.GetConfiguration(ctx, appName)
	require.NoError(t, err)

	var found bool
	for _, ev := range read.Configuration().EnvVars() {
		if ev.Key() == "STRIPE_API_KEY" {
			found = true
			assert.Equal(t, "cluster", ev.Backend())
		}
	}
	assert.True(t, found, "the referenced variable should be readable back")
}

// The EnvVarInput adapters are the other way a reference reaches config, and
// they drop the backend just as silently if it is not carried across.
func TestSetInitialEnvVarsPreservesTheSecretBackend(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	appInfo := &AppInfo{
		Log:     slog.Default(),
		EC:      ec,
		CPU:     &metrics.CPUUsage{},
		Mem:     &metrics.MemoryUsage{},
		HTTP:    &metrics.HTTPMetrics{},
		Secrets: pinningResolver{},
	}

	client := &app_v1alpha.CrudClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo)),
	}

	appName := "test-backend-initial"
	_, err := inmem.Client.Create(ctx, appName, &core_v1alpha.App{})
	require.NoError(t, err)

	nv := &app_v1alpha.NamedValue{}
	nv.SetKey("STRIPE_API_KEY")
	nv.SetValue("payments/stripe-key@x1A")
	nv.SetSensitive(true)
	nv.SetBackend("cluster")

	res, err := client.SetInitialEnvVars(ctx, appName, []*app_v1alpha.NamedValue{nv}, "")
	require.NoError(t, err)

	var cv core_v1alpha.ConfigVersion
	require.NoError(t, ec.GetById(ctx, entity.Id(res.ConfigVersionId()), &cv))

	require.Len(t, cv.Spec.Variables, 1)
	assert.Equal(t, "cluster", cv.Spec.Variables[0].Backend)
	assert.Equal(t, "payments/stripe-key@x1A", cv.Spec.Variables[0].Value)
}

// TestSetConfiguration_DropsAddonBindings covers the GetConfiguration round
// trip. GetConfiguration returns the runtime view, so a client that reads it,
// edits one variable and writes the whole set back sends the addon's binding
// along with everything else. Storing it would copy an addon credential into a
// version again, and because deprovisioning no longer rewrites stored config,
// that copy would outlive the addon.
func TestSetConfiguration_DropsAddonBindings(t *testing.T) {
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
	client := &app_v1alpha.CrudClient{Client: rpc.LocalClient(app_v1alpha.AdaptCrud(appInfo))}

	appName := "addon-roundtrip"
	appID, err := inmem.Client.Create(ctx, appName, &core_v1alpha.App{})
	require.NoError(t, err)

	// What a client sends back after reading the runtime view: its own variable
	// plus the addon's binding, which it never set and does not own.
	own := &app_v1alpha.NamedValue{}
	own.SetKey("LOG_LEVEL")
	own.SetValue("debug")
	own.SetSource(coreutil.SourceManual)

	binding := &app_v1alpha.NamedValue{}
	binding.SetKey("DATABASE_URL")
	binding.SetValue("postgres://from-the-addon")
	binding.SetSensitive(true)
	binding.SetSource(coreutil.SourceAddon)

	cfg := &app_v1alpha.Configuration{}
	cfg.SetEnvVars([]*app_v1alpha.NamedValue{own, binding})

	_, err = client.SetConfiguration(ctx, appName, cfg)
	require.NoError(t, err)

	var appCheck core_v1alpha.App
	require.NoError(t, ec.GetById(ctx, appID, &appCheck))

	var ver core_v1alpha.AppVersion
	require.NoError(t, ec.GetById(ctx, appCheck.ActiveVersion, &ver))

	stored, err := coreutil.ResolveConfig(ctx, ec.EAC(), &ver)
	require.NoError(t, err)

	keys := make(map[string]string, len(stored.Variables))
	for _, v := range stored.Variables {
		keys[v.Key] = v.Value
	}
	assert.Equal(t, "debug", keys["LOG_LEVEL"], "the client's own variable must be stored")
	assert.NotContains(t, keys, "DATABASE_URL",
		"an addon binding handed back by the client must not be written into a version")
}
