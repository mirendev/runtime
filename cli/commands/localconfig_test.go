package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func writeLocalAppConfig(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".miren"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".miren", "app.toml"), []byte(contents), 0o644))
	chdir(t, dir)
}

func TestLocalAppConfig(t *testing.T) {
	t.Run("broken config warns once across callers", func(t *testing.T) {
		writeLocalAppConfig(t, "name = 'demo'\npost_import = ''\n")

		out := captureStderr(t, func() {
			// Alias expansion, setup's LoadCluster, and the command body all
			// ask; the file is read once and reported once.
			_, err := LoadLocalAppConfig()
			require.Error(t, err)
			require.Nil(t, loadLocalAppConfigOrWarn())
			require.Nil(t, loadLocalAppConfigOrWarn())
			WarnLocalAppConfig()
		})
		require.Equal(t, 1, strings.Count(out, "warning:"), "stderr:\n%s", out)
		require.Contains(t, out, `unknown field "post_import"`)
	})

	t.Run("fatal report suppresses the warning", func(t *testing.T) {
		writeLocalAppConfig(t, "name = 'demo'\npost_import = ''\n")

		out := captureStderr(t, func() {
			var a AppCentric
			a.Dir = "."
			require.Error(t, a.Validate(nil))
			WarnLocalAppConfig()
		})
		require.Empty(t, out)
	})

	t.Run("fatal report through -d suppresses the warning", func(t *testing.T) {
		writeLocalAppConfig(t, "name = 'demo'\npost_import = ''\n")
		cwd, err := os.Getwd()
		require.NoError(t, err)

		out := captureStderr(t, func() {
			_, err := LoadLocalAppConfig()
			require.Error(t, err)
			var a AppCentric
			a.Dir = cwd
			require.Error(t, a.Validate(nil))
			WarnLocalAppConfig()
		})
		require.Empty(t, out)
	})

	t.Run("fatal report for another file leaves the warning pending", func(t *testing.T) {
		writeLocalAppConfig(t, "name = 'demo'\npost_import = ''\n")
		other := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(other, ".miren"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(other, ".miren", "app.toml"), []byte("name = 'other'\nbogus = 1\n"), 0o644))

		out := captureStderr(t, func() {
			_, err := LoadLocalAppConfig()
			require.Error(t, err)
			var a AppCentric
			a.Dir = other
			require.ErrorContains(t, a.Validate(nil), "bogus")
			WarnLocalAppConfig()
		})
		require.Equal(t, 1, strings.Count(out, "warning:"), "stderr:\n%s", out)
		require.Contains(t, out, "post_import")
	})

	t.Run("valid config is shared", func(t *testing.T) {
		writeLocalAppConfig(t, "name = 'demo'\n")

		out := captureStderr(t, func() {
			ac := loadLocalAppConfigOrWarn()
			require.NotNil(t, ac)
			require.Equal(t, "demo", ac.Name)
			again, err := LoadLocalAppConfig()
			require.NoError(t, err)
			require.Same(t, ac, again)
			WarnLocalAppConfig()
		})
		require.Empty(t, out)
	})

	t.Run("no config is silent", func(t *testing.T) {
		chdir(t, t.TempDir())

		out := captureStderr(t, func() {
			require.Nil(t, loadLocalAppConfigOrWarn())
			WarnLocalAppConfig()
		})
		require.Empty(t, out)
	})
}
