package build

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
)

// sagaTestHarness bundles the infrastructure each saga test needs:
// in-memory entity server, a Builder configured against it, a fresh
// StreamRegistry, and a registry+executor wired to the build-from-tar
// definition. Keeps each test self-contained and avoids global state.
type sagaTestHarness struct {
	t        *testing.T
	inmem    *testutils.InMemEntityServer
	builder  *Builder
	streams  *StreamRegistry
	statuses *StatusRegistry
	registry *saga.Registry
	executor *saga.Executor
	storage  saga.Storage
}

func newSagaTestHarness(t *testing.T) *sagaTestHarness {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	tempDir := t.TempDir()

	rpcClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
	builder := &Builder{
		Log:        log,
		EAS:        inmem.EAC,
		ec:         entityserver.NewClient(log, inmem.EAC),
		appClient:  app.NewClient(log, rpcClient),
		TempDir:    tempDir,
		cacheLocks: newAppLocks(),
		deploy:     deploylifecycle.NewTracker(log, inmem.EAC),
	}

	streams := NewStreamRegistry(tempDir, log)
	statuses := NewStatusRegistry()

	registry := saga.NewRegistry()
	// Use a test-mode registration that swaps in a stub buildImage so
	// we don't need a real BuildKit component for unit tests. The
	// stub returns a synthetic digest/artifact ID so downstream actions
	// have something deterministic to work with. The real buildImage
	// path is exercised by blackbox tests under iso.
	deps := &buildSagaDeps{builder: builder, streams: streams, statuses: statuses}
	if err := saga.Define(sagaBuildFromTar).
		Using(deps).
		Using(log).
		Action(actionReceiveTar, receiveTar).Undo(undoReceiveTar).
		Action(actionLoadSource, loadSource).Undo(undoLoadSource).
		Action(actionGetNextVer, getNextVersion).Undo(undoGetNextVersion).
		Action(actionBuildImage, stubBuildImage).Undo(undoBuildImage).
		Action(actionPrepareConfig, prepareConfig).Undo(undoPrepareConfig).
		Action(actionHandleEphemera, handleEphemeral).Undo(undoHandleEphemeral).
		Action(actionCreateConfigVer, createConfigVersion).Undo(undoCreateConfigVersion).
		Action(actionCreateVersion, createVersion).Undo(undoCreateVersion).
		Action(actionProvisionAddons, provisionAddons).Undo(undoProvisionAddons).
		Action(actionSetActiveVer, setActiveVersion).Undo(undoSetActiveVersion).
		Action(actionFinalize, finalize).Undo(undoFinalize).
		Action(actionBeginDeploy, beginDeployment).Undo(undoBeginDeployment).
		Action(actionRecordVersion, recordAppVersion).Undo(undoRecordAppVersion).
		Action(actionActivateDeploy, activateDeployment).Undo(undoActivateDeployment).
		RegisterTo(registry); err != nil {
		t.Fatalf("registering build saga: %v", err)
	}

	executor := saga.NewExecutor(
		saga.NewMemoryStorage(),
		saga.WithRegistry(registry),
		saga.WithLogger(log),
	)

	return &sagaTestHarness{
		t:        t,
		inmem:    inmem,
		builder:  builder,
		streams:  streams,
		statuses: statuses,
		registry: registry,
		executor: executor,
	}
}

// stubBuildImage replaces the real buildImage action in unit tests so
// we don't need a live BuildKit daemon. It returns a deterministic
// digest/artifact ID derived from the version name so the test can
// assert on what downstream actions saw, and it pre-creates the
// matching Artifact entity so any "locate artifact" code path the
// real action would have walked finds something. Real buildImage is
// exercised by blackbox tests under iso.
func stubBuildImage(ctx context.Context, in buildImageIn) (buildImageOut, error) {
	// Exercise the production direct-image action through the saga context. It
	// needs no BuildKit or Artifact fixture, which is precisely the contract
	// image-only deploys rely on. Source builds continue through the deterministic
	// stub below.
	if in.BuildStack.Stack == "image" {
		return buildImage(ctx, in)
	}

	deps := saga.Get[*buildSagaDeps](ctx)

	digest := "sha256:" + in.VersionName + "-digest"
	// Including VersionName (which itself carries a random suffix from
	// nextVersion) keeps the entity name unique when a test runs the
	// saga twice against the same in-memory store.
	artifactName := in.AppName + "-" + in.VersionName + "-stub"
	artifact := &core_v1alpha.Artifact{
		ManifestDigest: digest,
		App:            entity.Id(in.AppID),
		Status:         core_v1alpha.ACTIVE,
	}
	id, err := deps.builder.ec.Create(ctx, artifactName, artifact)
	if err != nil {
		return buildImageOut{}, err
	}
	return buildImageOut{
		ManifestDigest: digest,
		ArtifactID:     string(id),
		FinalImageURL:  "cluster.local:5000/" + in.AppName + ":" + artifactName,
		BuildResult: &BuildResult{
			ManifestDigest: digest,
			Entrypoint:     "echo hi",
			Command:        "",
			WorkingDir:     "/app",
		},
	}, nil
}

// dockerfileTarball returns a tar containing the bare minimum to satisfy
// every saga action up through getNextVersion: a .miren/app.toml at the
// path appconfig.AppConfigPath expects, and a Dockerfile.miren so stack
// detection short-circuits on the dockerfile path without invoking the
// auto-detector (which would fail without a recognized stack).
func dockerfileTarball(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		".miren/app.toml":  "name = 'demo'\n",
		"Dockerfile.miren": "FROM alpine\nCMD echo hi\n",
		"Procfile":         "web: echo hi\n",
	}
}

func imageOnlyTarball(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		".miren/app.toml": `name = "demo"

[services.web]
image = "nginx:alpine"
port = 80
`,
	}
}

func TestBuildSaga_HappyPath_RunsFullPipeline(t *testing.T) {
	ctx := context.Background()

	h := newSagaTestHarness(t)
	h.streams.Register("stream-1", makeTar(t, dockerfileTarball(t)))

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-1").
		WithID("test-happy-path").
		Execute(ctx)
	if err != nil {
		t.Fatalf("saga: %v", err)
	}

	// finalize cleans up the staged tar on success — the registry
	// should no longer have a path for this ID.
	if _, ok := h.streams.StagedPath("stream-1"); ok {
		t.Error("expected staged source to be cleaned up by finalize")
	}

	// get-next-version creates the App entity on first deploy.
	var application core_v1alpha.App
	if err := h.builder.ec.Get(ctx, "demo", &application); err != nil {
		t.Fatalf("expected app entity 'demo' to exist: %v", err)
	}
	// set-active-version should have populated the new version.
	if application.ActiveVersion == "" {
		t.Error("expected app to have an active version after build saga")
	}
}

func TestBuildSaga_MalformedGitInfoDoesNotBlockDeployment(t *testing.T) {
	ctx := context.Background()

	h := newSagaTestHarness(t)
	h.streams.Register("stream-malformed-git-info", makeTar(t, dockerfileTarball(t)))

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-malformed-git-info").
		Input("deploy_git_info_json", "{").
		WithID("test-malformed-git-info").
		Execute(ctx)
	require.NoError(t, err)

	var application core_v1alpha.App
	require.NoError(t, h.builder.ec.Get(ctx, "demo", &application))
	require.NotEmpty(t, application.ActiveVersion)

	var version core_v1alpha.AppVersion
	require.NoError(t, h.builder.ec.GetById(ctx, application.ActiveVersion, &version))
	assert.Empty(t, version.Source)
}

func TestBuildSaga_ImageOnly_SkipsBuildKitAndCreatesVersion(t *testing.T) {
	ctx := context.Background()

	h := newSagaTestHarness(t)
	h.streams.Register("stream-image", makeTar(t, imageOnlyTarball(t)))

	rec := &recordingSender{}
	h.statuses.Register("stream-image", rec)
	t.Cleanup(func() { h.statuses.Unregister("stream-image") })

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-image").
		WithID("test-image-only").
		Execute(ctx)
	require.NoError(t, err)

	var application core_v1alpha.App
	require.NoError(t, h.builder.ec.Get(ctx, "demo", &application))
	require.NotEmpty(t, application.ActiveVersion)

	var version core_v1alpha.AppVersion
	require.NoError(t, h.builder.ec.GetById(ctx, application.ActiveVersion, &version))
	assert.Equal(t, "docker.io/library/nginx:alpine", version.ImageUrl)
	assert.Empty(t, version.Artifact, "direct-image versions do not invent an Artifact")

	var configVersion core_v1alpha.ConfigVersion
	require.NoError(t, h.builder.ec.GetById(ctx, version.ConfigVersion, &configVersion))
	require.Len(t, configVersion.Spec.Services, 1)
	assert.Equal(t, "web", configVersion.Spec.Services[0].Name)
	assert.Equal(t, "nginx:alpine", configVersion.Spec.Services[0].Image)
	assert.Empty(t, configVersion.Spec.Services[0].Command)
	assert.Contains(t, rec.Images, "docker.io/library/nginx:alpine")
}

func TestImageSourceSelection(t *testing.T) {
	t.Run("web image captures explicit intent", func(t *testing.T) {
		ac := &appconfig.AppConfig{Services: map[string]*appconfig.ServiceConfig{
			"web": {Image: "example/web:v1"},
			"db":  {Image: "example/db:v1"},
		}}
		assert.Equal(t, "example/web:v1", webImageSource(ac))
		assert.Empty(t, webImageSource(nil))
	})

	tests := []struct {
		name    string
		config  *appconfig.AppConfig
		want    string
		wantErr string
	}{
		{name: "no config"},
		{
			name: "one non-web image is unambiguous",
			config: &appconfig.AppConfig{Services: map[string]*appconfig.ServiceConfig{
				"worker": {Image: "example/worker:v1"},
			}},
			want: "example/worker:v1",
		},
		{
			name: "several non-web images are ambiguous",
			config: &appconfig.AppConfig{Services: map[string]*appconfig.ServiceConfig{
				"worker": {Image: "example/worker:v1"},
				"cron":   {Image: "example/cron:v1"},
			}},
			wantErr: "set services.web.image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fallbackImageSource(tt.config)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectBuildStack_ImagePrecedence(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b := &Builder{Log: log}
	webImage := &appconfig.AppConfig{Services: map[string]*appconfig.ServiceConfig{
		"web": {Image: "nginx:alpine"},
	}}
	sidecarImage := &appconfig.AppConfig{Services: map[string]*appconfig.ServiceConfig{
		"db": {Image: "postgres:alpine"},
	}}
	writeNodeSource := func(t *testing.T, dir string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "index.js"), []byte("console.log('hello')\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node index.js"}}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644))
	}

	t.Run("web image when source is not buildable", func(t *testing.T) {
		stack, err := b.detectBuildStack(t.TempDir(), webImage, "demo", nil)
		require.NoError(t, err)
		assert.Equal(t, "image", stack.Stack)
		assert.Equal(t, "nginx:alpine", stack.Input)
	})

	t.Run("web image beats detected source", func(t *testing.T) {
		dir := t.TempDir()
		writeNodeSource(t, dir)
		stack, err := b.detectBuildStack(dir, webImage, "demo", nil)
		require.NoError(t, err)
		assert.Equal(t, "image", stack.Stack)
		assert.Equal(t, "nginx:alpine", stack.Input)
	})

	t.Run("sidecar image does not suppress detected source", func(t *testing.T) {
		dir := t.TempDir()
		writeNodeSource(t, dir)
		stack, err := b.detectBuildStack(dir, sidecarImage, "demo", nil)
		require.NoError(t, err)
		assert.Equal(t, "auto", stack.Stack)
		assert.Empty(t, stack.Input)
	})

	t.Run("one non-web image is a fallback", func(t *testing.T) {
		stack, err := b.detectBuildStack(t.TempDir(), sidecarImage, "demo", nil)
		require.NoError(t, err)
		assert.Equal(t, "image", stack.Stack)
		assert.Equal(t, "postgres:alpine", stack.Input)
	})

	t.Run("dockerfile retains precedence", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile.miren"), []byte("FROM alpine\n"), 0o644))
		stack, err := b.detectBuildStack(dir, webImage, "demo", nil)
		require.NoError(t, err)
		assert.Equal(t, "dockerfile", stack.Stack)
		assert.Equal(t, "Dockerfile.miren", stack.Input)
	})

	t.Run("configured dockerfile beats discovered default", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile.miren"), []byte("FROM alpine\n"), 0o644))
		config := &appconfig.AppConfig{
			Build:    &appconfig.BuildConfig{Dockerfile: "Dockerfile.production"},
			Services: webImage.Services,
		}
		stack, err := b.detectBuildStack(dir, config, "demo", nil)
		require.NoError(t, err)
		assert.Equal(t, "dockerfile", stack.Stack)
		assert.Equal(t, "Dockerfile.production", stack.Input)
	})

	t.Run("no source and no image keeps the existing error", func(t *testing.T) {
		_, err := b.detectBuildStack(t.TempDir(), &appconfig.AppConfig{}, "demo", nil)
		require.ErrorContains(t, err, "no supported stack detected for app demo")
	})
}

func TestBuildSaga_ReceiveTar_EmitsStatusUpdates(t *testing.T) {
	ctx := context.Background()

	h := newSagaTestHarness(t)
	h.streams.Register("stream-status", makeTar(t, dockerfileTarball(t)))

	rec := &recordingSender{}
	h.statuses.Register("stream-status", rec)
	t.Cleanup(func() { h.statuses.Unregister("stream-status") })

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-status").
		WithID("test-status-emit").
		Execute(ctx)
	if err != nil {
		t.Fatalf("saga: %v", err)
	}

	// receive-tar should have emitted both progress messages bracketing
	// the staging work. We don't pin exact equality on the entire slice
	// (later actions might emit too as the saga grows) — just verify
	// these two appeared in order.
	wantInOrder := []string{"Reading application data", "Preparing deployment"}
	if !containsInOrder(rec.Messages, wantInOrder) {
		t.Errorf("missing expected messages in order: got %v, want subsequence %v", rec.Messages, wantInOrder)
	}
}

func TestBuildSaga_Finalize_EmitsDeployWarnings(t *testing.T) {
	tests := []struct {
		name        string
		appConfig   string
		warningText string
		prepare     func(*testing.T, *sagaTestHarness)
	}{
		{
			name:        "local storage migration",
			appConfig:   "name = 'demo'\n",
			warningText: "Local storage data was automatically mounted",
			prepare: func(t *testing.T, h *sagaTestHarness) {
				h.builder.DataPath = t.TempDir()
				appRec, err := h.builder.appClient.Create(context.Background(), "demo")
				if err != nil {
					t.Fatalf("creating app: %v", err)
				}
				localDir := filepath.Join(h.builder.DataPath, "data", "local", appRec.ID.String())
				if err := os.MkdirAll(localDir, 0755); err != nil {
					t.Fatalf("creating local storage: %v", err)
				}
				if err := os.WriteFile(filepath.Join(localDir, "data.db"), []byte("existing data"), 0644); err != nil {
					t.Fatalf("seeding local storage: %v", err)
				}
			},
		},
		{
			name: "aliased local disks",
			appConfig: `name = "demo"

[[services.web.disks]]
name = "cache"
provider = "local"
mount_path = "/cache"

[[services.web.disks]]
name = "data"
provider = "local"
mount_path = "/data"
`,
			warningText: "Multiple local disks share one per-app store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			h := newSagaTestHarness(t)
			if tt.prepare != nil {
				tt.prepare(t, h)
			}

			files := dockerfileTarball(t)
			files[".miren/app.toml"] = tt.appConfig
			streamID := "stream-warning-" + strings.ReplaceAll(tt.name, " ", "-")
			h.streams.Register(streamID, makeTar(t, files))

			rec := &recordingSender{}
			h.statuses.Register(streamID, rec)
			t.Cleanup(func() { h.statuses.Unregister(streamID) })

			err := h.executor.Start(sagaBuildFromTar).
				Input("app_name", "demo").
				Input("stream_id", streamID).
				WithID("test-warning-" + strings.ReplaceAll(tt.name, " ", "-")).
				Execute(ctx)
			if err != nil {
				t.Fatalf("saga: %v", err)
			}

			var warning *recordedLog
			for i := range rec.Logs {
				if rec.Logs[i].Text == tt.warningText {
					warning = &rec.Logs[i]
					break
				}
			}
			if warning == nil {
				t.Fatalf("missing warning %q in logs: %v", tt.warningText, rec.Logs)
			}
			if warning.Level != "warn" {
				t.Errorf("warning level = %q, want warn", warning.Level)
			}
			fieldKeys := make(map[string]bool)
			for _, field := range warning.Fields {
				fieldKeys[field.Key()] = true
			}
			for _, key := range []string{"detail", "link"} {
				if !fieldKeys[key] {
					t.Errorf("warning fields missing %q: %v", key, warning.Fields)
				}
			}
		})
	}
}

// containsInOrder checks that all of `want` appear in `got` in the
// listed order (allowing other entries in between). Used so tests for
// individual actions tolerate additional status messages emitted by
// later actions in the same saga.
func containsInOrder(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// TestBuildSaga_FailedActivate_CompensatesEntities verifies the core
// saga guarantee: when a late action fails, prior entity-creating
// actions undo themselves in reverse. set-active-version is the last
// real side-effecting step; swapping it with a failing version should
// trigger create-version and create-config-version compensations,
// leaving the entity store free of orphaned ConfigVersion / AppVersion
// rows.
func TestBuildSaga_FailedActivate_CompensatesEntities(t *testing.T) {
	ctx := context.Background()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	rpcClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
	tempDir := t.TempDir()
	builder := &Builder{
		Log:        log,
		EAS:        inmem.EAC,
		ec:         entityserver.NewClient(log, inmem.EAC),
		appClient:  app.NewClient(log, rpcClient),
		TempDir:    tempDir,
		cacheLocks: newAppLocks(),
	}
	streams := NewStreamRegistry(tempDir, log)
	statuses := NewStatusRegistry()
	streams.Register("stream-fail", makeTar(t, dockerfileTarball(t)))

	// Swap setActiveVersion with a deterministic failure. Same signature,
	// same input/output keys — the framework can't tell the difference,
	// but the saga compensates everything after createConfigVersion.
	failingSetActive := func(ctx context.Context, in setActiveVersionIn) (setActiveVersionOut, error) {
		return setActiveVersionOut{}, errSimulatedActivate
	}

	registry := saga.NewRegistry()
	deps := &buildSagaDeps{builder: builder, streams: streams, statuses: statuses}
	if err := saga.Define(sagaBuildFromTar).
		Using(deps).
		Using(log).
		Action(actionReceiveTar, receiveTar).Undo(undoReceiveTar).
		Action(actionLoadSource, loadSource).Undo(undoLoadSource).
		Action(actionGetNextVer, getNextVersion).Undo(undoGetNextVersion).
		Action(actionBuildImage, stubBuildImage).Undo(undoBuildImage).
		Action(actionPrepareConfig, prepareConfig).Undo(undoPrepareConfig).
		Action(actionHandleEphemera, handleEphemeral).Undo(undoHandleEphemeral).
		Action(actionCreateConfigVer, createConfigVersion).Undo(undoCreateConfigVersion).
		Action(actionCreateVersion, createVersion).Undo(undoCreateVersion).
		Action(actionProvisionAddons, provisionAddons).Undo(undoProvisionAddons).
		Action(actionSetActiveVer, failingSetActive).Undo(undoSetActiveVersion).
		Action(actionFinalize, finalize).Undo(undoFinalize).
		RegisterTo(registry); err != nil {
		t.Fatalf("registering build saga: %v", err)
	}

	storage := saga.NewMemoryStorage()
	executor := saga.NewExecutor(
		storage,
		saga.WithRegistry(registry),
		saga.WithLogger(log),
	)

	err := executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-fail").
		WithID("test-failed-activate").
		Execute(ctx)
	if err == nil {
		t.Fatal("saga should have failed when set-active-version errors")
	}

	// Pull the saga execution back and verify both create-config-version
	// and create-version recorded UndoneAt timestamps. Then pull the
	// created entity IDs out of each action's output and confirm the
	// entities themselves are actually gone — UndoneAt only says we
	// called the undo, not that it succeeded.
	exec, err := storage.Get(ctx, "test-failed-activate")
	if err != nil {
		t.Fatalf("loading saga execution: %v", err)
	}
	for _, action := range []string{actionCreateConfigVer, actionCreateVersion} {
		r := exec.ExecutedActions[action]
		if r == nil {
			t.Errorf("expected %s to have run before failure", action)
			continue
		}
		if r.UndoneAt == nil {
			t.Errorf("expected %s to be undone, but UndoneAt is nil", action)
		}
	}

	var cvOut createConfigVersionOut
	if r := exec.ExecutedActions[actionCreateConfigVer]; r != nil && len(r.Output) > 0 {
		if err := json.Unmarshal(r.Output, &cvOut); err == nil && cvOut.ConfigVersionID != "" {
			var dummy core_v1alpha.ConfigVersion
			if err := builder.ec.GetById(ctx, entity.Id(cvOut.ConfigVersionID), &dummy); err == nil {
				t.Errorf("expected ConfigVersion %s to be deleted, but Get succeeded", cvOut.ConfigVersionID)
			}
		}
	}
	var avOut createVersionOut
	if r := exec.ExecutedActions[actionCreateVersion]; r != nil && len(r.Output) > 0 {
		if err := json.Unmarshal(r.Output, &avOut); err == nil && avOut.AppVersionID != "" {
			var dummy core_v1alpha.AppVersion
			if err := builder.ec.GetById(ctx, entity.Id(avOut.AppVersionID), &dummy); err == nil {
				t.Errorf("expected AppVersion %s to be deleted, but Get succeeded", avOut.AppVersionID)
			}
		}
	}

	// Staged source is cleaned up via undoReceiveTar.
	if _, ok := streams.StagedPath("stream-fail"); ok {
		t.Error("expected staged source to be cleaned up after compensation")
	}
}

var errSimulatedActivate = errors.New("simulated activate failure")

func TestBuildSaga_FailsWhenStreamUnavailable(t *testing.T) {
	ctx := context.Background()

	h := newSagaTestHarness(t)
	// Note: no Register call — simulates a crash before the stream arrived,
	// or a saga revived after the in-process stream was lost.

	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").
		Input("stream_id", "stream-gone").
		WithID("test-stream-gone").
		Execute(ctx)
	if err == nil {
		t.Fatal("saga should have failed when stream is unavailable")
	}
	// The framework serializes action errors to strings and reconstructs
	// them on failure, which breaks errors.Is chains. Match the message
	// so we still confirm the stream-unavailable signal made it through.
	if !strings.Contains(err.Error(), ErrStreamUnavailable.Error()) {
		t.Errorf("expected error to mention %q, got %v", ErrStreamUnavailable.Error(), err)
	}

}

// The saga orders actions by data dependency, not by registration order, so the
// gate's position has to be asserted rather than assumed. Everything about
// deploy tasks depends on landing in exactly one place: after addons exist,
// because a migration needs its database, and before ActiveVersion flips,
// because that placement is what makes a failed task a failed deploy rather
// than a half-promoted one.
func TestBuildSaga_DeployTasksGateSitsBetweenAddonsAndActivation(t *testing.T) {
	registry := saga.NewRegistry()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := registerBuildSaga(registry, nil, nil, nil, log); err != nil {
		t.Fatalf("registering build saga: %v", err)
	}

	def, ok := registry.Get(sagaBuildFromTar)
	if !ok {
		t.Fatal("build saga not registered")
	}

	order := def.ExecutionOrder()

	pos := func(action string) int {
		for i, name := range order {
			if name == action {
				return i
			}
		}
		t.Fatalf("action %q not in execution order: %v", action, order)
		return -1
	}

	addons := pos(actionProvisionAddons)
	tasks := pos(actionRunDeployTasks)
	activate := pos(actionSetActiveVer)

	if addons >= tasks {
		t.Errorf("deploy tasks must run after addons are provisioned (a migration needs its database); order: %v", order)
	}
	if tasks >= activate {
		t.Errorf("deploy tasks must gate the version flip, or a failed task leaves a half-promoted deploy; order: %v", order)
	}
}
