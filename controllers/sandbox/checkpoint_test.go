package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	runcopts "github.com/containerd/containerd/api/types/runc/options"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

type fakeCheckpointRuntime struct {
	save, restore, fallback func() error
}

func (f fakeCheckpointRuntime) Save(context.Context, *compute.Sandbox, *entity.Meta) error {
	return f.save()
}
func (f fakeCheckpointRuntime) Restore(context.Context, *compute.Sandbox, *entity.Meta) error {
	return f.restore()
}
func (f fakeCheckpointRuntime) Fallback(context.Context, *compute.Sandbox) error { return f.fallback() }

func TestCheckpointLifecycle(t *testing.T) {
	for _, status := range []compute.SandboxStatus{compute.HIBERNATING, compute.RESTORING} {
		for _, fail := range []bool{false, true} {
			t.Run(string(status)+map[bool]string{true: "/failure", false: "/success"}[fail], func(t *testing.T) {
				ctx := context.Background()
				es, cleanup := testutils.NewInMemEntityServer(t)
				defer cleanup()
				sb := &compute.Sandbox{Status: status}
				id, err := es.Client.Create(ctx, "checkpoint-lifecycle", sb)
				require.NoError(t, err)
				sb.ID = id
				c := &SandboxController{Log: testutils.TestLogger(t), EAC: es.EAC}
				c.ops = &sandboxOps{ctrl: c}
				fallback := false
				operation := func() error {
					current, _, err := c.ops.GetSandbox(ctx, id.String())
					require.NoError(t, err)
					require.Equal(t, status, current.Status, "must not publish ready before runtime completes")
					if fail {
						return errors.New("runtime unavailable")
					}
					return nil
				}
				c.checkpoints = fakeCheckpointRuntime{save: operation, restore: operation, fallback: func() error {
					fallback = true
					return nil
				}}
				require.NoError(t, c.Create(ctx, sb, nil))
				require.Equal(t, fail, fallback)
				current, _, err := c.ops.GetSandbox(ctx, id.String())
				require.NoError(t, err)
				want := status
				if !fail {
					want = compute.HIBERNATED
					if status == compute.RESTORING {
						want = compute.RUNNING
					}
				}
				require.Equal(t, want, current.Status)
				if !fail {
					// A stale work item must not replay an already completed operation.
					c.checkpoints = fakeCheckpointRuntime{}
					require.NoError(t, c.Create(ctx, sb, nil))
				}
			})
		}
	}
}

func TestCheckpointExitFence(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	c := &SandboxController{Log: testutils.TestLogger(t), EAC: es.EAC}
	c.ops = &sandboxOps{ctrl: c}
	at := time.Now().Truncate(time.Millisecond)
	for i, tc := range []struct {
		status     compute.SandboxStatus
		exit       time.Time
		generation time.Time
		ignored    bool
	}{
		{compute.HIBERNATING, at, at, true}, {compute.HIBERNATED, at, at, true},
		{compute.RESTORING, at.Add(-time.Second), at, true}, {compute.RUNNING, at, at, true},
		{compute.RUNNING, at.Add(time.Second), at, false},
		{compute.RESTORING, at.Add(time.Second), time.Time{}, true},
		{compute.RUNNING, at.Add(time.Second), time.Time{}, true},
	} {
		sb := &compute.Sandbox{Status: tc.status, RestoredAt: at}
		id, err := es.Client.Create(ctx, fmt.Sprintf("exit-fence-%d", i), sb)
		require.NoError(t, err)
		result, err := c.recordExit(ctx, id, tc.generation, compute.Exit{At: tc.exit, Code: 17, Container: "web"})
		require.NoError(t, err)
		require.Equal(t, tc.ignored, result == nil)
		current, _, err := c.ops.GetSandbox(ctx, id.String())
		require.NoError(t, err)
		if tc.ignored {
			require.Equal(t, tc.status, current.Status)
		} else {
			require.Equal(t, compute.STOPPED, current.Status)
			require.EqualValues(t, 17, current.Exit.Code)
		}
	}
}

func TestCheckpointManifestInvalidation(t *testing.T) {
	at := time.Now()
	sb := &compute.Sandbox{Spec: compute.SandboxSpec{Version: "version/one"}}
	spec, err := typeurl.MarshalAny(&specs.Spec{Hostname: "original"})
	require.NoError(t, err)
	info := containers.Container{SnapshotKey: "private-original", Spec: spec}
	m := checkpointManifest{SpecHash: checkpointHash(sb.Spec), ConfigHash: "config-one", OCIHash: checkpointHash(spec.GetValue()), SnapshotKey: info.SnapshotKey, BootID: "boot-one", CreatedAt: at}
	require.True(t, m.matches(sb, info, "config-one", "boot-one", at.Add(computeapi.CheckpointRetention-time.Nanosecond)))
	require.False(t, m.matches(sb, info, "config-two", "boot-one", at), "same spec cannot revive different live config")
	withoutConfig := m
	withoutConfig.ConfigHash = ""
	require.False(t, withoutConfig.matches(sb, info, "config-one", "boot-one", at), "older checkpoint files are not trusted")
	require.False(t, m.matches(sb, info, "config-one", "boot-one", at.Add(computeapi.CheckpointRetention)))
	require.False(t, m.matches(sb, info, "config-one", "boot-two", at))
	require.False(t, m.matches(sb, info, "config-one", "boot-one", at.Add(-time.Second)))
	sb.Spec.Version = "version/two"
	require.False(t, m.matches(sb, info, "config-one", "boot-one", at))
	sb.Spec.Version = "version/one"
	info.SnapshotKey = "other-instance"
	require.False(t, m.matches(sb, info, "config-one", "boot-one", at))
	info.SnapshotKey = "private-original"
	info.Spec, err = typeurl.MarshalAny(&specs.Spec{Hostname: "changed"})
	require.NoError(t, err)
	require.False(t, m.matches(sb, info, "config-one", "boot-one", at))
}

func TestCheckpointCurrentConfigAndCredentials(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	app, err := es.Client.Create(ctx, "checkpoint-config-app", &core_v1alpha.App{})
	require.NoError(t, err)
	base := core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web", Port: 3211}},
		Variables: []core_v1alpha.ConfigSpecVariables{{Key: "FEATURE", Value: "original"}, {Key: "GREETING", Value: "hello"}}}
	createConfig := func(cfg core_v1alpha.ConfigSpec) entity.Id {
		resp, err := es.EAC.Create(ctx, entity.New((&core_v1alpha.ConfigVersion{App: app, Spec: cfg}).Encode).Attrs())
		require.NoError(t, err)
		return entity.Id(resp.Id())
	}
	configID := createConfig(base)
	ver := &core_v1alpha.AppVersion{App: app, Version: "one", ImageUrl: "app:v1", ConfigVersion: configID}
	ver.ID, err = es.Client.Create(ctx, "checkpoint-config-version", ver)
	require.NoError(t, err)
	desired, err := appspec.Build(nil, appspec.Options{AppID: app, AppName: "checkpoint-config-app", Version: ver,
		Config: &base, Service: "web", Image: ver.ImageUrl})
	require.NoError(t, err)
	sb := &compute.Sandbox{Spec: *desired}
	pool := &compute.SandboxPool{App: app, Service: "web", SandboxSpec: *desired,
		ReferencedByVersions: []entity.Id{ver.ID}}
	c := &SandboxController{EAC: es.EAC}
	hash, err := c.checkpointConfigHash(ctx, sb, pool)
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	second, err := c.checkpointConfigHash(ctx, sb, pool)
	require.NoError(t, err)
	require.Equal(t, hash, second, "random map iteration order must not invalidate identical env")

	// Even an in-place edit to the referenced ConfigVersion cannot hide behind
	// the unchanged AppVersion ID and pool spec.
	changedInPlace := base
	changedInPlace.Variables = slices.Clone(base.Variables)
	changedInPlace.Variables[0].Value = "updated"
	_, err = es.EAC.Patch(ctx, entity.New(entity.DBId, configID,
		(&core_v1alpha.ConfigVersion{Spec: changedInPlace}).Encode).Attrs(), 0)
	require.NoError(t, err)
	_, err = c.checkpointConfigHash(ctx, sb, pool)
	require.ErrorContains(t, err, "version, config, or environment changed")
	_, err = es.EAC.Patch(ctx, entity.New(entity.DBId, configID,
		(&core_v1alpha.ConfigVersion{Spec: base}).Encode).Attrs(), 0)
	require.NoError(t, err)

	// A new config with the same process spec still invalidates the image.
	changed := base
	changed.Services = slices.Clone(base.Services)
	changed.Services[0].Concurrency.ScaleDownDelay = "1m"
	_, err = es.EAC.Patch(ctx, entity.New(entity.DBId, ver.ID,
		(&core_v1alpha.AppVersion{ConfigVersion: createConfig(changed)}).Encode).Attrs(), 0)
	require.NoError(t, err)
	newHash, err := c.checkpointConfigHash(ctx, sb, pool)
	require.NoError(t, err)
	require.NotEqual(t, hash, newHash)

	_, err = es.EAC.Patch(ctx, entity.New(entity.DBId, ver.ID,
		(&core_v1alpha.AppVersion{Version: "two"}).Encode).Attrs(), 0)
	require.NoError(t, err)
	_, err = c.checkpointConfigHash(ctx, sb, pool)
	require.ErrorContains(t, err, "version, config, or environment changed")
	_, err = es.EAC.Patch(ctx, entity.New(entity.DBId, ver.ID,
		(&core_v1alpha.AppVersion{Version: "one"}).Encode).Attrs(), 0)
	require.NoError(t, err)

	changed.Variables = slices.Clone(base.Variables)
	changed.Variables[0].Value = "revised"
	_, err = es.EAC.Patch(ctx, entity.New(entity.DBId, ver.ID,
		(&core_v1alpha.AppVersion{ConfigVersion: createConfig(changed)}).Encode).Attrs(), 0)
	require.NoError(t, err)
	_, err = c.checkpointConfigHash(ctx, sb, pool)
	require.ErrorContains(t, err, "version, config, or environment changed")

	for _, credential := range []struct {
		name    string
		change  func(*core_v1alpha.ConfigSpec)
		message string
	}{
		{"sensitive literal", func(cfg *core_v1alpha.ConfigSpec) { cfg.Variables[0].Sensitive = true }, "cannot refresh"},
		{"addon binding", func(cfg *core_v1alpha.ConfigSpec) { cfg.Variables[0].Source = "addon" }, "config, or environment changed"},
		{"service secret", func(cfg *core_v1alpha.ConfigSpec) {
			cfg.Services[0].Env = []core_v1alpha.ConfigSpecServicesEnv{{Key: "TOKEN", Value: "short-lived", Sensitive: true}}
		}, "cannot refresh"},
	} {
		t.Run(credential.name, func(t *testing.T) {
			cfg := base
			cfg.Variables = slices.Clone(base.Variables)
			cfg.Services = slices.Clone(base.Services)
			credential.change(&cfg)
			_, err := es.EAC.Patch(ctx, entity.New(entity.DBId, ver.ID,
				(&core_v1alpha.AppVersion{ConfigVersion: createConfig(cfg)}).Encode).Attrs(), 0)
			require.NoError(t, err)
			_, err = c.checkpointConfigHash(ctx, sb, pool)
			require.ErrorContains(t, err, credential.message)
		})
	}
}

func TestCheckpointOptionsAndFreshNamespaces(t *testing.T) {
	var info containerd.CheckpointTaskInfo
	require.NoError(t, checkpointTaskOptions("/private/images")(&info))
	opts := info.Options.(*runcopts.CheckpointOptions)
	require.True(t, opts.Exit)
	require.False(t, opts.OpenTcp)
	require.False(t, opts.ExternalUnixSockets)
	require.Equal(t, "/private/images", opts.ImagePath)
	spec := &specs.Spec{Linux: &specs.Linux{Namespaces: []specs.LinuxNamespace{
		{Type: specs.NetworkNamespace, Path: "/proc/111/ns/net"},
		{Type: specs.IPCNamespace, Path: "/proc/111/ns/ipc"},
		{Type: specs.UTSNamespace, Path: "/proc/111/ns/uts"},
		{Type: specs.TimeNamespace, Path: "/proc/111/ns/time"},
		{Type: specs.PIDNamespace},
	}}}
	rebindCheckpointNamespaces(spec, 987)
	for i, name := range []string{"net", "ipc", "uts", "time"} {
		require.Equal(t, "/proc/987/ns/"+name, spec.Linux.Namespaces[i].Path)
	}
	require.Empty(t, spec.Linux.Namespaces[4].Path)
}

func TestCheckpointConcurrentStopWins(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	sb := &compute.Sandbox{Status: compute.RESTORING}
	id, err := es.Client.Create(ctx, "stop-during-restore", sb)
	require.NoError(t, err)
	sb.ID = id
	c := &SandboxController{Log: testutils.TestLogger(t), EAC: es.EAC}
	c.ops = &sandboxOps{ctrl: c}
	fallback := false
	c.checkpoints = fakeCheckpointRuntime{restore: func() error {
		_, err := c.ops.PatchSandbox(ctx, entity.New(entity.DBId, id,
			(&compute.Sandbox{Status: compute.STOPPED}).Encode).Attrs(), 0)
		return err
	}, fallback: func() error { fallback = true; return nil }}
	require.NoError(t, c.Create(ctx, sb, nil))
	require.True(t, fallback, "a revoked restore must clean up its partial process")
	current, _, err := c.ops.GetSandbox(ctx, id.String())
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, current.Status)
}

func TestCheckpointOrphanCleanup(t *testing.T) {
	c := &SandboxController{DataPath: t.TempDir()}
	for _, id := range []entity.Id{"sandbox/retained", "sandbox/deleted"} {
		dir := c.checkpointPath(id)
		require.NoError(t, os.MkdirAll(dir, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "memory.img"), []byte("private process memory"), 0600))
	}
	require.NoError(t, c.pruneCheckpointFiles(map[string]bool{entity.Id("sandbox/retained").PathSafe(): true}))
	require.DirExists(t, c.checkpointPath("sandbox/retained"))
	require.NoDirExists(t, c.checkpointPath("sandbox/deleted"))
}
