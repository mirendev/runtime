//go:build linux

package victorialogs

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/testutils"
)

const legacyVictoriaLogsImage = "oci.miren.cloud/victoria-logs@sha256:30ae01fbc0ef51e7b929c16f17b85112e763bb8b3427d3b2bc6495df0cbad7ac"

func directoryHashes(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	hashes := map[string][32]byte{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err == nil {
			hashes[rel] = sha256.Sum256(data)
		}
		return err
	}))
	return hashes
}

func TestContainerSpecChangeRecreatesButRestartReuses(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}

	cc, err := containerd.New(containerdx.DefaultSocket)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cc.Close()) })
	var active *VictoriaLogsComponent
	t.Cleanup(func() {
		if active != nil {
			require.NoError(t, active.Stop(context.Background()))
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	namespace := "vl-spec-" + identity.NewID()
	dataRoot := t.TempDir()
	port := testutils.GetFreePort(t)
	config := VictoriaLogsConfig{HTTPPort: port, RetentionPeriod: "7d"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	firstCtx, stopFirstMonitor := context.WithCancel(ctx)
	first := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = first
	require.NoError(t, first.Start(firstCtx, config))
	original := first.GetContainer()
	originalInfo, err := original.Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataRoot, "victorialogs", "mount-marker"), []byte("preserved"), 0o600))
	stopFirstMonitor()
	require.NoError(t, first.StopTask(ctx, first.GetTask()))

	secondCtx, stopSecondMonitor := context.WithCancel(ctx)
	restarted := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = restarted
	require.NoError(t, restarted.Start(secondCtx, config))
	restartedInfo, err := restarted.GetContainer().Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.Equal(t, originalInfo.CreatedAt, restartedInfo.CreatedAt, "an unchanged spec should reuse the container")

	stopSecondMonitor()
	require.NoError(t, restarted.StopTask(ctx, restarted.GetTask()))
	require.NoError(t, restarted.GetContainer().Update(namespaces.WithNamespace(ctx, namespace), func(_ context.Context, _ *containerd.Client, record *containers.Container) error {
		delete(record.Labels, componentSpecLabel)
		return nil
	}))

	legacyCtx, stopLegacyMonitor := context.WithCancel(ctx)
	legacyRecreated := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = legacyRecreated
	require.NoError(t, legacyRecreated.Start(legacyCtx, config))
	legacyInfo, err := legacyRecreated.GetContainer().Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.NotEqual(t, restartedInfo.CreatedAt, legacyInfo.CreatedAt, "an unlabeled pre-upgrade container should be recreated")
	stopLegacyMonitor()
	require.NoError(t, legacyRecreated.StopTask(ctx, legacyRecreated.GetTask()))

	recreated := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = recreated
	changed := config
	changed.RetentionPeriod = "14d"
	recreatedCtx, stopRecreatedMonitor := context.WithCancel(ctx)
	require.NoError(t, recreated.Start(recreatedCtx, changed))
	recreatedInfo, err := recreated.GetContainer().Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.NotEqual(t, restartedInfo.CreatedAt, recreatedInfo.CreatedAt, "a changed spec should recreate the container")
	marker, err := os.ReadFile(filepath.Join(dataRoot, "victorialogs", "mount-marker"))
	require.NoError(t, err)
	require.Equal(t, "preserved", string(marker), "recreation must not replace the bind-mounted data directory")

	spec, err := recreated.GetContainer().Spec(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.True(t, slices.Contains(spec.Process.Args, "-retentionPeriod=14d"))
	var dataMountSource string
	for _, mount := range spec.Mounts {
		if mount.Destination == "/victoria-logs-data" {
			dataMountSource = mount.Source
			break
		}
	}
	require.Equal(t, filepath.Join(dataRoot, "victorialogs"), dataMountSource)

	// Returning to a previous config on the same image must not restore an
	// older snapshot and discard records written under the intermediate config.
	require.NoError(t, os.WriteFile(filepath.Join(dataRoot, "victorialogs", "newer-marker"), []byte("newer"), 0o600))
	stopRecreatedMonitor()
	require.NoError(t, recreated.StopTask(ctx, recreated.GetTask()))
	backToOriginal := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = backToOriginal
	require.NoError(t, backToOriginal.Start(ctx, config))
	require.FileExists(t, filepath.Join(dataRoot, "victorialogs", "newer-marker"))
}

func TestUpgradeAndSnapshotRestore(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}

	cc, err := containerd.New(containerdx.DefaultSocket)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cc.Close()) })
	var active *VictoriaLogsComponent
	t.Cleanup(func() {
		if active != nil {
			require.NoError(t, active.Stop(context.Background()))
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	namespace := "vl-upgrade-" + identity.NewID()
	dataRoot := t.TempDir()
	port := testutils.GetFreePort(t)
	config := VictoriaLogsConfig{HTTPPort: port, RetentionPeriod: "7d"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oldImage := victoriaLogsImage
	defer func() { victoriaLogsImage = oldImage }()

	victoriaLogsImage = legacyVictoriaLogsImage
	legacyCtx, stopLegacyMonitor := context.WithCancel(ctx)
	legacy := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = legacy
	require.NoError(t, legacy.Start(legacyCtx, config))
	writer := observability.NewPersistentLogWriter(legacy.HTTPEndpoint(), 30*time.Second)
	reader := observability.NewLogReader(legacy.HTTPEndpoint(), 30*time.Second)
	preUpgrade := observability.LogEntry{Timestamp: time.Now().Add(-time.Minute), Stream: observability.Stdout, Body: "before upgrade"}
	require.NoError(t, writer.WriteEntry("app/upgrade-test", preUpgrade))
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 1
	}, 30*time.Second, 200*time.Millisecond)

	stopLegacyMonitor()
	require.NoError(t, legacy.StopTask(ctx, legacy.GetTask()))
	legacyInfo, err := legacy.GetContainer().Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	legacySpec := legacyInfo.Labels[componentSpecLabel]
	preUpgradeHashes := directoryHashes(t, filepath.Join(dataRoot, "victorialogs"))

	victoriaLogsImage = imagerefs.VictoriaLogs
	upgraded := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = upgraded
	require.NoError(t, upgraded.Start(ctx, config))
	upgradedInfo, err := upgraded.GetContainer().Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.Equal(t, imagerefs.VictoriaLogs, upgradedInfo.Image)
	snapshot := backupPath(filepath.Join(dataRoot, "victorialogs"), legacySpec)
	require.Equal(t, preUpgradeHashes, directoryHashes(t, snapshot))

	reader = observability.NewLogReader(upgraded.HTTPEndpoint(), 30*time.Second)
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 1 && entries[0].Body == "before upgrade"
	}, 30*time.Second, 200*time.Millisecond, "the upgraded image must open and query v1.0 data")
	writer = observability.NewPersistentLogWriter(upgraded.HTTPEndpoint(), 30*time.Second)
	require.NoError(t, writer.WriteEntry("app/upgrade-test", observability.LogEntry{
		Timestamp: time.Now(), Stream: observability.Stdout, Body: "after upgrade",
	}))
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 2 && entries[1].Body == "after upgrade"
	}, 30*time.Second, 200*time.Millisecond, "the upgraded image must persist newly ingested data")
	require.NoError(t, upgraded.Stop(ctx))
	restarted := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = restarted
	require.NoError(t, restarted.Start(ctx, config))
	reader = observability.NewLogReader(restarted.HTTPEndpoint(), 30*time.Second)
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 2 && entries[0].Body == "before upgrade" && entries[1].Body == "after upgrade"
	}, 30*time.Second, 200*time.Millisecond, "new data must remain readable after restart")
	require.NoError(t, restarted.Stop(ctx))
	require.Equal(t, preUpgradeHashes, directoryHashes(t, snapshot), "new writes and restarts must not mutate backup files")
	victoriaLogsImage = legacyVictoriaLogsImage
	rolledBack := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = rolledBack
	require.NoError(t, rolledBack.Start(ctx, config))
	rolledBackInfo, err := rolledBack.GetContainer().Info(namespaces.WithNamespace(ctx, namespace))
	require.NoError(t, err)
	require.Equal(t, legacyVictoriaLogsImage, rolledBackInfo.Image)
	require.Equal(t, preUpgradeHashes, directoryHashes(t, snapshot))
	restoredHashes := directoryHashes(t, filepath.Join(dataRoot, "victorialogs"))
	delete(preUpgradeHashes, componentSpecFile)
	delete(restoredHashes, componentSpecFile) // The live state retains both images for a safe roll-forward.
	require.Equal(t, preUpgradeHashes, restoredHashes)
	require.DirExists(t, filepath.Join(dataRoot, "victorialogs.replaced-"+upgradedInfo.Labels[componentSpecLabel]))

	reader = observability.NewLogReader(rolledBack.HTTPEndpoint(), 30*time.Second)
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 1 && entries[0].Body == "before upgrade"
	}, 30*time.Second, 200*time.Millisecond, "rollback must restore the stopped pre-upgrade snapshot")
	require.NoError(t, observability.NewPersistentLogWriter(rolledBack.HTTPEndpoint(), 30*time.Second).WriteEntry("app/upgrade-test", observability.LogEntry{
		Timestamp: time.Now(), Stream: observability.Stdout, Body: "during rollback",
	}))
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 2 && entries[1].Body == "during rollback"
	}, 30*time.Second, 200*time.Millisecond)
	require.NoError(t, rolledBack.Stop(ctx))
	victoriaLogsImage = imagerefs.VictoriaLogs
	forward := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	active = forward
	require.NoError(t, forward.Start(ctx, config))
	reader = observability.NewLogReader(forward.HTTPEndpoint(), 30*time.Second)
	require.Eventually(t, func() bool {
		entries, readErr := reader.Read(ctx, "app/upgrade-test")
		return readErr == nil && len(entries) == 2 && entries[0].Body == "before upgrade" && entries[1].Body == "during rollback"
	}, 30*time.Second, 200*time.Millisecond, "rolling forward must migrate live data, not restore the prior v1.52 snapshot")
}

func TestBackupFailureDoesNotModifyLiveData(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "victorialogs")
	require.NoError(t, os.Mkdir(data, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("original"), 0o600))
	require.NoError(t, os.Symlink("log", filepath.Join(data, "unsupported-link")))
	err := preserveOrRestoreData(data, "", "", true)
	require.ErrorContains(t, err, "unexpected non-regular")
	require.FileExists(t, filepath.Join(data, "log"))
	require.NoDirExists(t, backupPath(data, ""))
}

func TestSymlinkedDataCannotProduceEmptyBackup(t *testing.T) {
	root := t.TempDir()
	realData := filepath.Join(root, "real-data")
	require.NoError(t, os.Mkdir(realData, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(realData, "log"), []byte("preserve me"), 0o600))
	data := filepath.Join(root, "victorialogs")
	require.NoError(t, os.Symlink(realData, data))
	err := preserveOrRestoreData(data, "", "", true)
	require.ErrorContains(t, err, "not a real directory")
	require.FileExists(t, filepath.Join(data, "log"))
	require.NoDirExists(t, backupPath(data, ""))
}

func TestRepeatedRollbackKeepsBothReplacedStores(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "victorialogs")
	require.NoError(t, os.Mkdir(data, 0o755))
	a := "sha256:" + strings.Repeat("a", 64)
	b := "sha256:" + strings.Repeat("b", 64)
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("A"), 0o600))
	require.NoError(t, preserveOrRestoreData(data, a, "", true))
	for _, body := range []string{"B first", "B second"} {
		require.NoError(t, os.Remove(filepath.Join(data, "log")))
		require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte(body), 0o600))
		require.NoError(t, preserveOrRestoreData(data, b, backupPath(data, a), true))
		current, err := os.ReadFile(filepath.Join(data, "log"))
		require.NoError(t, err)
		require.Equal(t, "A", string(current))
		if body == "B first" {
			require.NoError(t, preserveOrRestoreData(data, a, "", true))
		}
	}
	quarantines, err := filepath.Glob(data + ".replaced-" + b + "*")
	require.NoError(t, err)
	require.Len(t, quarantines, 2)
	for i, path := range quarantines {
		contents, readErr := os.ReadFile(filepath.Join(path, "log"))
		require.NoError(t, readErr)
		require.Equal(t, []string{"B first", "B second"}[i], string(contents))
	}
}

func TestImageChangeRefreshesRevisitedSpecBackup(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "victorialogs")
	require.NoError(t, os.Mkdir(data, 0o755))
	a := "sha256:" + strings.Repeat("a", 64)
	b := "sha256:" + strings.Repeat("b", 64)
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("old"), 0o600))
	require.NoError(t, preserveOrRestoreData(data, a, "", false))
	require.NoError(t, os.Remove(filepath.Join(data, "log")))
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("new"), 0o600))
	require.NoError(t, preserveOrRestoreData(data, b, backupPath(data, a), false))
	newer, err := os.ReadFile(filepath.Join(data, "log"))
	require.NoError(t, err)
	require.Equal(t, "new", string(newer), "a same-image config flip must not restore the old backup")
	require.NoError(t, preserveOrRestoreData(data, a, "", true))
	latest, err := os.ReadFile(filepath.Join(backupPath(data, a), "log"))
	require.NoError(t, err)
	require.Equal(t, "new", string(latest), "rollback backup must include pre-upgrade writes")
	archived, err := filepath.Glob(backupPath(data, a) + ".superseded-*")
	require.NoError(t, err)
	require.Len(t, archived, 1)
	older, err := os.ReadFile(filepath.Join(archived[0], "log"))
	require.NoError(t, err)
	require.Equal(t, "old", string(older))
}

func TestRollbackFindsImageAfterConfigChange(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "victorialogs")
	require.NoError(t, os.Mkdir(data, 0o755))
	a := "sha256:" + strings.Repeat("a", 64)
	b := "sha256:" + strings.Repeat("b", 64)
	c := "sha256:" + strings.Repeat("c", 64)
	d := "sha256:" + strings.Repeat("d", 64) // image A with different retention
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("old image"), 0o600))
	require.NoError(t, writeSpecState(filepath.Join(data, componentSpecFile), a, "image-A"))
	require.NoError(t, preserveOrRestoreData(data, a, "", true))
	require.NoError(t, writeSpecState(filepath.Join(data, componentSpecFile), b, "image-B"))
	require.NoError(t, os.Remove(filepath.Join(data, "log")))
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("new image"), 0o600))
	require.NoError(t, preserveOrRestoreData(data, b, "", false))
	require.NoError(t, writeSpecState(filepath.Join(data, componentSpecFile), c, "image-B"))
	history := []string{"image-A", "image-B"}
	require.True(t, isRollbackImage(history, "image-B", "image-A"))
	restore, err := restoreBackup(data, d, "image-A")
	require.NoError(t, err)
	require.Equal(t, backupPath(data, a), restore, "full spec differs but image A is recoverable")
	require.NoError(t, preserveOrRestoreData(data, c, restore, true))
	contents, err := os.ReadFile(filepath.Join(data, "log"))
	require.NoError(t, err)
	require.Equal(t, "old image", string(contents))
	require.DirExists(t, data+".replaced-"+c)
	require.NoError(t, writeSpecState(filepath.Join(data, componentSpecFile), d, "image-A", history...))
	require.NoError(t, os.Remove(filepath.Join(data, "log")))
	require.NoError(t, os.WriteFile(filepath.Join(data, "log"), []byte("during rollback"), 0o600))
	other, err := restoreBackup(data, "sha256:"+strings.Repeat("e", 64), "image-B")
	require.NoError(t, err)
	require.Equal(t, backupPath(data, b), other)
	require.False(t, isRollbackImage(history, "image-A", "image-B"), "a prior image-B backup must not make roll-forward a restore")
	require.NoError(t, preserveOrRestoreData(data, d, "", true))
	contents, err = os.ReadFile(filepath.Join(data, "log"))
	require.NoError(t, err)
	require.Equal(t, "during rollback", string(contents))
}

func TestStoppedLegacyDataIsBackedUpWithoutContainer(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cc, err := containerd.New(containerdx.DefaultSocket)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cc.Close()) })
	dataRoot := t.TempDir()
	namespace := "vl-legacy-" + identity.NewID()
	config := VictoriaLogsConfig{HTTPPort: testutils.GetFreePort(t), RetentionPeriod: "7d"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oldImage := victoriaLogsImage
	defer func() { victoriaLogsImage = oldImage }()
	victoriaLogsImage = legacyVictoriaLogsImage
	old := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	require.NoError(t, old.Start(ctx, config))
	require.NoError(t, observability.NewPersistentLogWriter(old.HTTPEndpoint(), 30*time.Second).WriteEntry("app/legacy", observability.LogEntry{
		Timestamp: time.Now().Add(-time.Minute), Stream: observability.Stdout, Body: "legacy record",
	}))
	require.Eventually(t, func() bool {
		entries, readErr := observability.NewLogReader(old.HTTPEndpoint(), 30*time.Second).Read(ctx, "app/legacy")
		return readErr == nil && len(entries) == 1
	}, 30*time.Second, 200*time.Millisecond)
	require.NoError(t, old.Stop(ctx))
	require.NoError(t, os.Remove(filepath.Join(dataRoot, "victorialogs", componentSpecFile)))
	// An older installation left this state outside the data directory. A
	// manual restore of v1.0 data must not trust its stale v1.52 identity.
	require.NoError(t, writeSpecState(filepath.Join(dataRoot, componentSpecFile),
		"sha256:"+strings.Repeat("b", 64), imagerefs.VictoriaLogs))
	before := directoryHashes(t, filepath.Join(dataRoot, "victorialogs"))
	victoriaLogsImage = imagerefs.VictoriaLogs
	upgraded := NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	t.Cleanup(func() { require.NoError(t, upgraded.Stop(context.Background())) })
	require.NoError(t, upgraded.Start(ctx, config))
	require.Equal(t, before, directoryHashes(t, filepath.Join(dataRoot, "victorialogs.backup-legacy")))
	require.Eventually(t, func() bool {
		entries, readErr := observability.NewLogReader(upgraded.HTTPEndpoint(), 30*time.Second).Read(ctx, "app/legacy")
		return readErr == nil && len(entries) == 1 && entries[0].Body == "legacy record"
	}, 30*time.Second, 200*time.Millisecond)
}
