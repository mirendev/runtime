package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s %v: %s", name, args, out)
	return string(out)
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
}

// isolateVCS pins identity and config for both git and jj in the process
// environment. GetInfo shells out without an explicit env, so the developer's
// own config would otherwise leak in; a jj rule like "commits by other
// authors are immutable" then makes snapshots behave differently than in CI.
func isolateVCS(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "Ada Lovelace", "GIT_AUTHOR_EMAIL": "ada@example.com",
		"GIT_COMMITTER_NAME": "Ada Lovelace", "GIT_COMMITTER_EMAIL": "ada@example.com",
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null",
	} {
		t.Setenv(k, v)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(cfg, []byte("[user]\nname = \"Ada Lovelace\"\nemail = \"ada@example.com\"\n"), 0o644))
	t.Setenv("JJ_CONFIG", cfg)
}

func TestGetInfoGit(t *testing.T) {
	requireBinary(t, "git")
	isolateVCS(t)
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "remote", "add", "origin", "https://example.com/acme/web.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.txt"), []byte("v1\n"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-q", "-m", "first commit")

	info, err := GetInfo(dir)
	require.NoError(t, err)
	require.Len(t, info.SHA, 40)
	require.Equal(t, "main", info.Branch)
	require.False(t, info.IsDirty)
	require.Empty(t, info.WorkingTreeHash)
	require.Equal(t, "first commit", info.CommitMessage)
	require.Equal(t, "Ada Lovelace", info.CommitAuthor)
	require.Equal(t, "ada@example.com", info.CommitEmail)
	require.Equal(t, "https://example.com/acme/web.git", info.RemoteURL)
	_, err = time.Parse(time.RFC3339, info.CommitTimestamp)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.txt"), []byte("v2\n"), 0o644))
	info, err = GetInfo(dir)
	require.NoError(t, err)
	require.True(t, info.IsDirty)
	require.Len(t, info.WorkingTreeHash, 8)
}

// A secondary workspace from `jj workspace add` has .jj/ but no .git/, so git
// cannot see it at all. The jj path must report the same shape git would for
// a colocated repo: the parent commit, dirty iff @ carries changes.
func TestGetInfoJJWorkspace(t *testing.T) {
	requireBinary(t, "jj")
	isolateVCS(t)
	root := t.TempDir()
	main := filepath.Join(root, "main")
	run(t, root, "jj", "git", "init", main)
	run(t, main, "jj", "git", "remote", "add", "origin", "https://example.com/acme/web.git")
	require.NoError(t, os.WriteFile(filepath.Join(main, "app.txt"), []byte("v1\n"), 0o644))
	run(t, main, "jj", "commit", "-m", "first commit")
	run(t, main, "jj", "bookmark", "create", "main", "-r", "@-")

	ws := filepath.Join(root, "ws")
	run(t, main, "jj", "workspace", "add", ws)
	_, err := os.Stat(filepath.Join(ws, ".git"))
	require.True(t, os.IsNotExist(err), "workspace must not be colocated for this test to mean anything")

	// The fresh workspace sits on an empty @ over the parent, i.e. clean.
	info, err := GetInfo(ws)
	require.NoError(t, err)
	parentSHA := run(t, ws, "jj", "log", "-r", "@-", "--no-graph", "-T", "commit_id")
	require.Equal(t, parentSHA, info.SHA)
	require.Equal(t, "main", info.Branch)
	require.False(t, info.IsDirty)
	require.Empty(t, info.WorkingTreeHash)
	require.Equal(t, "first commit", info.CommitMessage)
	require.Equal(t, "Ada Lovelace", info.CommitAuthor)
	require.Equal(t, "ada@example.com", info.CommitEmail)
	require.Equal(t, "https://example.com/acme/web.git", info.RemoteURL)
	_, err = time.Parse(time.RFC3339, info.CommitTimestamp)
	require.NoError(t, err)

	// Editing a file lands in @ on the next snapshot, which is jj's dirty state.
	require.NoError(t, os.WriteFile(filepath.Join(ws, "app.txt"), []byte("v2\n"), 0o644))
	info, err = GetInfo(ws)
	require.NoError(t, err)
	require.Equal(t, parentSHA, info.SHA, "sha stays on the parent while @ is in flight")
	require.True(t, info.IsDirty)
	require.Len(t, info.WorkingTreeHash, 8)
}

func TestGetInfoNoVCS(t *testing.T) {
	_, err := GetInfo(t.TempDir())
	require.Error(t, err)
}
