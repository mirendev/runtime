package appspec

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

func baseOptions() Options {
	return Options{
		AppID:   entity.Id("app/demo"),
		AppName: "demo",
		Version: &core_v1alpha.AppVersion{
			ID:         entity.Id("app_version/v1"),
			Version:    "v1",
			AdminToken: "tok",
		},
		Config: &core_v1alpha.ConfigSpec{
			StartDirectory: "/srv",
			Services: []core_v1alpha.ConfigSpecServices{
				{Name: "web", Command: "bin/server"},
			},
		},
		Service: "web",
		Image:   "registry/demo:v1",
	}
}

// envMap collapses the container's env slice, which is built from a Go map and
// therefore has no stable order.
func envMap(t *testing.T, spec *compute_v1alpha.SandboxSpec) map[string]string {
	t.Helper()
	require.Len(t, spec.Container, 1)

	out := make(map[string]string)
	for _, e := range spec.Container[0].Env {
		k, v, ok := strings.Cut(e, "=")
		require.True(t, ok, "malformed env entry %q", e)
		out[k] = v
	}
	return out
}

func TestBuildRequiresVersionAndConfig(t *testing.T) {
	opts := baseOptions()
	opts.Version = nil
	_, err := Build(nil, opts)
	assert.Error(t, err)

	opts = baseOptions()
	opts.Config = nil
	_, err = Build(nil, opts)
	assert.Error(t, err)
}

// With every run-specific option left at its zero value, Build must produce
// what the deployment launcher produced before this package existed. That is
// the regression gate for the extraction.
func TestBuildDefaultsMatchTheLauncher(t *testing.T) {
	spec, err := Build(nil, baseOptions())
	require.NoError(t, err)

	assert.Equal(t, entity.Id("app_version/v1"), spec.Version)
	assert.Equal(t, "app/demo", spec.LogEntity)
	assert.Equal(t, types.LabelSet("miren.stage", "app-run", "miren.service", "web"), spec.LogAttribute)
	assert.Empty(t, spec.RestartPolicy, "the historical default is to allow restarts")

	require.Len(t, spec.Container, 1)
	c := spec.Container[0]
	assert.Equal(t, "app", c.Name)
	assert.Equal(t, "registry/demo:v1", c.Image)
	assert.Equal(t, "/srv", c.Directory)
	assert.Equal(t, "bin/server", c.Command)
	assert.False(t, c.Stdin)
	assert.False(t, c.Tty)

	env := envMap(t, spec)
	assert.Equal(t, "demo", env["MIREN_APP"])
	assert.Equal(t, "v1", env["MIREN_VERSION"])
	assert.Equal(t, "tok", env["ADMIN_TOKEN"])
	assert.Equal(t, "3000", env["PORT"], "web defaults to 3000")
}

func TestBuildDefaultsStartDirectory(t *testing.T) {
	opts := baseOptions()
	opts.Config.StartDirectory = ""

	spec, err := Build(nil, opts)
	require.NoError(t, err)
	assert.Equal(t, "/app", spec.Container[0].Directory)
}

func TestBuildPrependsEntrypointToTheCommand(t *testing.T) {
	opts := baseOptions()
	opts.Config.Entrypoint = "/cnb/lifecycle/launcher"

	spec, err := Build(nil, opts)
	require.NoError(t, err)
	assert.Equal(t, "/cnb/lifecycle/launcher bin/server", spec.Container[0].Command)
}

// An explicit command replaces the service's, but still gets the entrypoint:
// that prefix is what makes a stack-built image runnable at all, so a task
// command needs it just as much as a service command does.
func TestBuildCommandOverrideStillGetsTheEntrypoint(t *testing.T) {
	opts := baseOptions()
	opts.Config.Entrypoint = "/cnb/lifecycle/launcher"
	opts.Command = "bin/rails db:migrate"

	spec, err := Build(nil, opts)
	require.NoError(t, err)
	assert.Equal(t, "/cnb/lifecycle/launcher bin/rails db:migrate", spec.Container[0].Command)
}

func TestBuildCommandOverrideWithoutEntrypoint(t *testing.T) {
	opts := baseOptions()
	opts.Command = "bin/reindex"

	spec, err := Build(nil, opts)
	require.NoError(t, err)
	assert.Equal(t, "bin/reindex", spec.Container[0].Command)
}

// Layering order is the part most easily broken by a refactor: per-service env
// overrides global config, and the platform's own variables override both.
func TestBuildEnvLayering(t *testing.T) {
	opts := baseOptions()
	opts.Config.Variables = []core_v1alpha.ConfigSpecVariables{
		{Key: "SHARED", Value: "global"},
		{Key: "ONLY_GLOBAL", Value: "yes"},
		{Key: "PORT", Value: "9999"},
		{Key: "ADMIN_TOKEN", Value: "forged"},
	}
	opts.Config.Services[0].Env = []core_v1alpha.ConfigSpecServicesEnv{
		{Key: "SHARED", Value: "service"},
		{Key: "ADMIN_TOKEN", Value: "also forged"},
	}

	spec, err := Build(nil, opts)
	require.NoError(t, err)

	env := envMap(t, spec)
	assert.Equal(t, "service", env["SHARED"], "per-service env overrides global")
	assert.Equal(t, "yes", env["ONLY_GLOBAL"])
	assert.Equal(t, "tok", env["ADMIN_TOKEN"], "user config cannot forge a system variable")
	assert.Equal(t, "3000", env["PORT"], "user config cannot forge PORT")
}

func TestBuildExtraEnvCannotShadowSystemVariables(t *testing.T) {
	opts := baseOptions()
	opts.ExtraEnv = []string{"TASK_NAME=migrate", "ADMIN_TOKEN=forged"}

	spec, err := Build(nil, opts)
	require.NoError(t, err)

	assert.Equal(t, "migrate", envMap(t, spec)["TASK_NAME"])

	// Both entries are present, but the system one is appended last and so is
	// the value the process sees.
	env := spec.Container[0].Env
	last := -1
	for i, e := range env {
		if strings.HasPrefix(e, "ADMIN_TOKEN=") {
			last = i
		}
	}
	require.NotEqual(t, -1, last)
	assert.Equal(t, "ADMIN_TOKEN=tok", env[last])
}

func TestBuildPorts(t *testing.T) {
	t.Run("scalar port", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services[0].Port = 8080

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		require.Len(t, spec.Container[0].Port, 1)
		assert.Equal(t, int64(8080), spec.Container[0].Port[0].Port)
		assert.Equal(t, "http", spec.Container[0].Port[0].Name)
		assert.Equal(t, "8080", envMap(t, spec)["PORT"])
	})

	t.Run("multi-port picks the first http for PORT", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services[0].Ports = []core_v1alpha.ConfigSpecServicesPorts{
			{Port: 9000, Name: "grpc", Type: "tcp"},
			{Port: 8080, Name: "http", Type: "http"},
		}

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Equal(t, "8080", envMap(t, spec)["PORT"])
	})

	t.Run("web without an http port gets 3000 added", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services[0].Ports = []core_v1alpha.ConfigSpecServicesPorts{
			{Port: 9000, Name: "grpc", Type: "tcp"},
		}

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Len(t, spec.Container[0].Port, 2)
		assert.Equal(t, "3000", envMap(t, spec)["PORT"])
	})

	t.Run("a non-web service gets no default port", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services = []core_v1alpha.ConfigSpecServices{{Name: "worker", Command: "bin/worker"}}
		opts.Service = "worker"

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Empty(t, spec.Container[0].Port)
		assert.NotContains(t, envMap(t, spec), "PORT")
	})

	t.Run("port_timeout carries through", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services[0].PortTimeout = "120s"

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Equal(t, "120s", spec.PortWaitTimeout)
	})
}

// A run must declare no ports at all. The sandbox controller waits for declared
// ports to bind and kills the sandbox when they don't -- so a migration
// inheriting web's default 3000 would be executed and then reported as a failed
// startup.
func TestBuildSkipPorts(t *testing.T) {
	opts := baseOptions()
	opts.Config.Services[0].Port = 8080
	opts.Config.Services[0].PortTimeout = "120s"
	opts.SkipPorts = true

	spec, err := Build(nil, opts)
	require.NoError(t, err)

	assert.Empty(t, spec.Container[0].Port)
	assert.Empty(t, spec.PortWaitTimeout, "nothing will bind, so nothing should be waited for")
	assert.NotContains(t, envMap(t, spec), "PORT", "PORT follows from having a port")
}

func TestBuildSkipPortsOnWebService(t *testing.T) {
	opts := baseOptions()
	opts.SkipPorts = true

	spec, err := Build(nil, opts)
	require.NoError(t, err)
	assert.Empty(t, spec.Container[0].Port, "even web's 3000 default is suppressed")
}

func TestBuildDisks(t *testing.T) {
	withDisk := func(mode string) Options {
		opts := baseOptions()
		opts.Config.Services[0].Concurrency = core_v1alpha.ConfigSpecServicesConcurrency{
			Mode: mode, NumInstances: 1,
		}
		opts.Config.Services[0].Disks = []core_v1alpha.ConfigSpecServicesDisks{
			{Name: "data", MountPath: "/data", SizeGb: 10},
		}
		return opts
	}

	t.Run("fixed service attaches a miren disk", func(t *testing.T) {
		spec, err := Build(nil, withDisk("fixed"))
		require.NoError(t, err)
		require.Len(t, spec.Volume, 1)
		assert.Equal(t, "miren", spec.Volume[0].Provider)
		require.Len(t, spec.Container[0].Mount, 1)
		assert.Equal(t, "/data", spec.Container[0].Mount[0].Destination)
	})

	t.Run("autoscaling service skips a miren disk", func(t *testing.T) {
		spec, err := Build(nil, withDisk("auto"))
		require.NoError(t, err)
		assert.Empty(t, spec.Volume, "miren disks need a single writer")
	})

	t.Run("local disks are attached regardless of mode", func(t *testing.T) {
		opts := withDisk("auto")
		opts.Config.Services[0].Disks[0].Provider = core_v1alpha.ConfigSpecServicesDisksLOCAL

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		require.Len(t, spec.Volume, 1)
		assert.Equal(t, "local", spec.Volume[0].Provider)
	})

	t.Run("SkipDisks suppresses everything", func(t *testing.T) {
		opts := withDisk("fixed")
		opts.SkipDisks = true

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Empty(t, spec.Volume)
		assert.Empty(t, spec.Container[0].Mount)
	})
}

func TestBuildRunShape(t *testing.T) {
	opts := baseOptions()
	opts.Service = ""
	opts.Command = "bin/rails db:migrate"
	opts.SkipPorts = true
	opts.SkipDisks = true
	opts.Stdin = true
	opts.RestartPolicy = compute_v1alpha.SandboxSpecNEVER
	opts.LogAttrs = types.LabelSet(
		"miren.stage", "run",
		"miren.run", "run/demo-migrate-abc",
		"miren.task", "migrate",
	)

	spec, err := Build(nil, opts)
	require.NoError(t, err)

	assert.Equal(t, compute_v1alpha.SandboxSpecNEVER, spec.RestartPolicy)
	assert.Empty(t, spec.Container[0].Port)
	assert.Empty(t, spec.Volume)
	assert.True(t, spec.Container[0].Stdin, "a run is attachable even when started detached")
	assert.Equal(t, "bin/rails db:migrate", spec.Container[0].Command)

	// The run id rides in on the log attributes, which the sandbox controller
	// copies verbatim onto every log entry -- that is what makes a run's output
	// findable without touching the log pipeline.
	var keys []string
	for _, l := range spec.LogAttribute {
		keys = append(keys, l.Key)
	}
	assert.True(t, slices.Contains(keys, "miren.run"))

	// Still the app's environment: that is the premise of running in its image.
	env := envMap(t, spec)
	assert.Equal(t, "demo", env["MIREN_APP"])
	assert.Equal(t, "tok", env["ADMIN_TOKEN"])
}

func TestBuildShutdownTimeout(t *testing.T) {
	t.Run("from the service", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services[0].Concurrency = core_v1alpha.ConfigSpecServicesConcurrency{ShutdownTimeout: "30s"}

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Equal(t, "30s", spec.Container[0].ShutdownTimeout)
	})

	t.Run("explicit override wins", func(t *testing.T) {
		opts := baseOptions()
		opts.Config.Services[0].Concurrency = core_v1alpha.ConfigSpecServicesConcurrency{ShutdownTimeout: "30s"}
		opts.ShutdownTimeout = "5m"

		spec, err := Build(nil, opts)
		require.NoError(t, err)
		assert.Equal(t, "5m", spec.Container[0].ShutdownTimeout)
	})
}

// A run names no service, so nothing service-derived may leak into its spec.
func TestBuildWithNoMatchingService(t *testing.T) {
	opts := baseOptions()
	opts.Service = "nonexistent"
	opts.Command = "bin/task"

	spec, err := Build(nil, opts)
	require.NoError(t, err)
	assert.Empty(t, spec.Container[0].Port)
	assert.Empty(t, spec.Volume)
	assert.Equal(t, "bin/task", spec.Container[0].Command)
}

func TestIsSystemEnvVar(t *testing.T) {
	assert.True(t, IsSystemEnvVar("PORT"))
	assert.True(t, IsSystemEnvVar("ADMIN_TOKEN"))
	assert.True(t, IsSystemEnvVar("MIREN_APP"))
	assert.False(t, IsSystemEnvVar("DATABASE_URL"))
}

// A run names a task, not a service, so task env is the only path its declared
// environment has. It used to be stored at build time and read back nowhere,
// which silently dropped the credentials a task was declared to carry -- the
// RFD's own worked example among them.
func TestBuildMergesTaskEnv(t *testing.T) {
	opts := baseOptions()
	opts.Service = ""
	opts.Task = "session"
	opts.Config.Variables = []core_v1alpha.ConfigSpecVariables{
		{Key: "SHARED", Value: "global"},
		{Key: "ONLY_GLOBAL", Value: "kept"},
	}
	opts.Config.Tasks = []core_v1alpha.ConfigSpecTasks{
		{Name: "session", Env: []core_v1alpha.ConfigSpecTasksEnv{
			{Key: "ANTHROPIC_API_KEY", Value: "sk-test"},
			{Key: "SHARED", Value: "task-wins"},
		}},
		{Name: "other", Env: []core_v1alpha.ConfigSpecTasksEnv{
			{Key: "NOT_MINE", Value: "no"},
		}},
	}

	spec, err := Build(nil, opts)
	require.NoError(t, err)

	env := envMap(t, spec)
	assert.Equal(t, "sk-test", env["ANTHROPIC_API_KEY"])
	assert.Equal(t, "task-wins", env["SHARED"], "task env overrides the app's globals")
	assert.Equal(t, "kept", env["ONLY_GLOBAL"])
	assert.NotContains(t, env, "NOT_MINE", "another task's env must not leak in")
}

// System-managed vars are appended after everything else precisely so a task
// cannot shadow them; ADMIN_TOKEN in particular is the app's own credential.
func TestBuildTaskEnvCannotShadowSystemVariables(t *testing.T) {
	opts := baseOptions()
	opts.Service = ""
	opts.Task = "session"
	opts.Config.Tasks = []core_v1alpha.ConfigSpecTasks{
		{Name: "session", Env: []core_v1alpha.ConfigSpecTasksEnv{
			{Key: "ADMIN_TOKEN", Value: "stolen"},
		}},
	}

	spec, err := Build(nil, opts)
	require.NoError(t, err)

	assert.Equal(t, "tok", envMap(t, spec)["ADMIN_TOKEN"])
}
