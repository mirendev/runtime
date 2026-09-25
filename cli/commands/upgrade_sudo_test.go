package commands

import (
	"io/fs"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/release"
)

func TestSudoArgv(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}

	t.Run("passes the original arguments through intact", func(t *testing.T) {
		args := []string{"upgrade", "--channel", "main", "--version", "v0.16.0", "--force", "-v"}
		got := sudoArgv("/usr/bin/sudo", "/usr/local/bin/miren", args, env(nil))
		assert.Equal(t, append([]string{"/usr/bin/sudo", "/usr/local/bin/miren"}, args...), got)
	})

	t.Run("forwards allowlisted variables through env", func(t *testing.T) {
		got := sudoArgv("/usr/bin/sudo", "/usr/local/bin/miren", []string{"upgrade"}, env(map[string]string{
			release.AssetBaseURLEnv: "http://assets.local:8080",
			"MIREN_CONFIG":          "/home/me/.config/miren/clientconfig.yaml",
		}))
		assert.Equal(t, []string{
			"/usr/bin/sudo", "env", "MIREN_ASSET_BASE_URL=http://assets.local:8080",
			"/usr/local/bin/miren", "upgrade",
		}, got)
	})
}

func TestShellJoin(t *testing.T) {
	assert.Equal(t,
		`sudo env MIREN_ASSET_BASE_URL=http://h:1/x /usr/local/bin/miren upgrade --version 'v1 beta' '' 'it'\''s'`,
		shellJoin([]string{"sudo", "env", "MIREN_ASSET_BASE_URL=http://h:1/x", "/usr/local/bin/miren", "upgrade", "--version", "v1 beta", "", "it's"}),
	)
}

func TestDaemonSudoSteps(t *testing.T) {
	serverBinary := serverDaemon.manager().InstallPath

	t.Run("server mentions the etcd snapshot and runner hand-off", func(t *testing.T) {
		steps := daemonSudoSteps(serverDaemon, serverBinary)
		assert.Contains(t, steps, "snapshot etcd first when it is embedded (the default), so a failed upgrade can roll back the data too; with external etcd there is no snapshot, and rollback restores only the binary")
		assert.Contains(t, steps, "ask this cluster's runners, if any, to upgrade to the same build")
		for _, s := range steps {
			assert.NotContains(t, s, "copy the new build over this CLI", "the CLI is the server binary here")
		}
	})

	t.Run("runner has no etcd and names its own service", func(t *testing.T) {
		steps := daemonSudoSteps(runnerDaemon, "/home/me/.miren/release/miren")
		for _, s := range steps {
			assert.NotContains(t, s, "etcd")
		}
		assert.Contains(t, steps, "restart the miren-runner systemd service, rolling back if it does not come back healthy")
		assert.Contains(t, steps, "copy the new build over this CLI at /home/me/.miren/release/miren (the previous one is kept as /home/me/.miren/release/miren.old)")
	})
}

func TestTargetDaemon(t *testing.T) {
	testCases := []struct {
		name  string
		check bool
		user  bool
		root  bool
		want  string
	}{
		// The runner's config is root-only, so reading it here would fail
		// before the sudo offer is ever reached.
		{name: "non-root upgrade leaves the runner target to the sudo re-run", want: ""},
		{name: "--user never needs the runner target", user: true, root: true, want: ""},
		{name: "root upgrade resolves the runner target", root: true, want: "runner"},
		{name: "--check resolves the runner target", check: true, want: "runner"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, targetDaemon(runnerDaemon, tc.check, tc.user, tc.root).name)
		})
	}
}

type statInfo struct{ st *syscall.Stat_t }

func (s statInfo) Name() string       { return "miren" }
func (s statInfo) Size() int64        { return 0 }
func (s statInfo) Mode() fs.FileMode  { return 0o755 }
func (s statInfo) ModTime() time.Time { return time.Time{} }
func (s statInfo) IsDir() bool        { return false }
func (s statInfo) Sys() any           { return s.st }

func TestOwnerToRestore(t *testing.T) {
	user := statInfo{&syscall.Stat_t{Uid: 1000, Gid: 1000}}

	uid, gid, ok := ownerToRestore(user, 0)
	assert.True(t, ok, "root replacing a user's CLI hands it back")
	assert.Equal(t, 1000, uid)
	assert.Equal(t, 1000, gid)

	_, _, ok = ownerToRestore(user, 1000)
	assert.False(t, ok, "a non-root install already creates files as the caller")

	_, _, ok = ownerToRestore(statInfo{&syscall.Stat_t{}}, 0)
	assert.False(t, ok, "a root-owned CLI stays root-owned")
}
