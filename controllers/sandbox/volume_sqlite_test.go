package sandbox

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
)

// newSqliteVolumeTestController builds a controller with only what the guards
// below need. They all reject before touching the entity store or the disk, so
// no other dependency has to be stood up.
func newSqliteVolumeTestController(t *testing.T) *SandboxController {
	t.Helper()
	return &SandboxController{
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		DataPath: t.TempDir(),
	}
}

func sqliteVolume() compute.SandboxSpecVolume {
	return compute.SandboxSpecVolume{
		Name:      "state",
		Provider:  "sqlite",
		MountPath: "/data",
	}
}

// A read-only mount cannot host a WAL database: SQLite needs to write its
// -wal and -shm sidecars even to read. Failing here keeps it a clear error
// rather than an opaque one from inside SQLite at app start.
func TestConfigureSqliteVolumeRejectsReadOnly(t *testing.T) {
	c := newSqliteVolumeTestController(t)

	vol := sqliteVolume()
	vol.ReadOnly = true

	sb := &compute.Sandbox{Spec: compute.SandboxSpec{Version: "app_version/demo"}}

	_, err := c.configureSqliteVolume(context.Background(), sb, vol)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be read_only")
}

// A sqlite disk is identified by (app, id). Without an app there is no stable
// namespace, and two unrelated sandboxes declaring the same disk name would
// collide on one backup key with two writers.
func TestConfigureSqliteVolumeRequiresAppVersion(t *testing.T) {
	c := newSqliteVolumeTestController(t)

	sb := &compute.Sandbox{} // no Spec.Version

	_, err := c.configureSqliteVolume(context.Background(), sb, sqliteVolume())
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an app version")

	// Nothing should have been created on the way to that error.
	entries, rerr := os.ReadDir(filepath.Join(c.DataPath, "data"))
	require.True(t, rerr != nil || len(entries) == 0,
		"a rejected sqlite volume must not leave directories behind")
}

func TestConfigureSqliteVolumeRequiresMountPath(t *testing.T) {
	c := newSqliteVolumeTestController(t)

	vol := sqliteVolume()
	vol.MountPath = ""

	sb := &compute.Sandbox{Spec: compute.SandboxSpec{Version: "app_version/demo"}}

	_, err := c.configureSqliteVolume(context.Background(), sb, vol)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing mount_path")
}

// db_file and id both become path components, so the spec is re-checked even
// though app.toml validation already rejects these.
func TestConfigureSqliteVolumeRejectsUnsafeSpecValues(t *testing.T) {
	sb := &compute.Sandbox{Spec: compute.SandboxSpec{Version: "app_version/demo"}}

	for _, tc := range []struct {
		name   string
		mutate func(*compute.SandboxSpecVolume)
		want   string
	}{
		{"absolute db_file", func(v *compute.SandboxSpecVolume) { v.DbFile = "/etc/passwd" }, "invalid db_file"},
		{"escaping db_file", func(v *compute.SandboxSpecVolume) { v.DbFile = "../escape.db" }, "invalid db_file"},
		{"nested db_file", func(v *compute.SandboxSpecVolume) { v.DbFile = "sub/app.db" }, "must be a filename"},
		{"escaping id", func(v *compute.SandboxSpecVolume) { v.SqliteId = "../escape" }, "invalid sqlite id"},
		{"id with separator", func(v *compute.SandboxSpecVolume) { v.SqliteId = "a/b" }, "invalid sqlite id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newSqliteVolumeTestController(t)
			vol := sqliteVolume()
			tc.mutate(&vol)

			_, err := c.configureSqliteVolume(context.Background(), sb, vol)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}
