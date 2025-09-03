package deploygating

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDeployAllowed(t *testing.T) {
	t.Run("fails when directory does not exist", func(t *testing.T) {
		remedy, err := CheckDeployAllowed("/non/existent/path")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deployment directory does not exist")
		assert.Contains(t, remedy, "Ensure you are running the deploy command from the correct directory")
	})

	t.Run("fails when path is not a directory", func(t *testing.T) {
		// Create a temporary file
		tmpFile, err := os.CreateTemp("", "test-file")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		remedy, err := CheckDeployAllowed(tmpFile.Name())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deployment path is not a directory")
		assert.Contains(t, remedy, "Provide a valid directory path for deployment")
	})

	t.Run("fails when no web service defined", func(t *testing.T) {
		tmpDir := t.TempDir()

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no 'web' service defined")
		assert.Contains(t, remedy, "Option 1: Add to .miren/app.toml")
		assert.Contains(t, remedy, "Option 2: Add to Procfile")
		assert.Contains(t, remedy, "PORT to bind to the correct port")
	})

	t.Run("succeeds with web service in app.toml", func(t *testing.T) {
		tmpDir := t.TempDir()
		mirenDir := filepath.Join(tmpDir, ".miren")
		err := os.MkdirAll(mirenDir, 0755)
		require.NoError(t, err)

		appToml := `
[services.web]
command = "python -m http.server"

[services.worker]  
command = "python worker.py"
`
		appTomlPath := filepath.Join(mirenDir, "app.toml")
		err = os.WriteFile(appTomlPath, []byte(appToml), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.NoError(t, err)
		assert.Empty(t, remedy)
	})

	t.Run("succeeds with web service in Procfile", func(t *testing.T) {
		tmpDir := t.TempDir()

		procfileContent := "web: python -m http.server\nworker: python worker.py\n"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.NoError(t, err)
		assert.Empty(t, remedy)
	})

	t.Run("fails when only non-web services defined in app.toml", func(t *testing.T) {
		tmpDir := t.TempDir()
		mirenDir := filepath.Join(tmpDir, ".miren")
		err := os.MkdirAll(mirenDir, 0755)
		require.NoError(t, err)

		appToml := `
[services.worker]
command = "python worker.py"

[services.background]  
command = "python background.py"
`
		appTomlPath := filepath.Join(mirenDir, "app.toml")
		err = os.WriteFile(appTomlPath, []byte(appToml), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no 'web' service defined")
		assert.Contains(t, remedy, "Option 1: Add to .miren/app.toml")
	})

	t.Run("fails when only non-web services defined in Procfile", func(t *testing.T) {
		tmpDir := t.TempDir()

		procfileContent := "worker: python worker.py\nbackground: python background.py\n"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no 'web' service defined")
		assert.Contains(t, remedy, "Option 2: Add to Procfile")
	})

	t.Run("prefers app.toml over Procfile when both exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create .miren/app.toml with web service
		mirenDir := filepath.Join(tmpDir, ".miren")
		err := os.MkdirAll(mirenDir, 0755)
		require.NoError(t, err)

		appToml := `
[services.web]
command = "from-app-toml"
`
		appTomlPath := filepath.Join(mirenDir, "app.toml")
		err = os.WriteFile(appTomlPath, []byte(appToml), 0644)
		require.NoError(t, err)

		// Create Procfile without web service
		procfileContent := "worker: python worker.py\n"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err = os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		// Should succeed because app.toml has web service
		remedy, err := CheckDeployAllowed(tmpDir)
		assert.NoError(t, err)
		assert.Empty(t, remedy)
	})

	t.Run("fails with invalid app.toml", func(t *testing.T) {
		tmpDir := t.TempDir()
		mirenDir := filepath.Join(tmpDir, ".miren")
		err := os.MkdirAll(mirenDir, 0755)
		require.NoError(t, err)

		// Invalid TOML syntax
		appToml := `
[services.web
command = "missing closing bracket"
`
		appTomlPath := filepath.Join(mirenDir, "app.toml")
		err = os.WriteFile(appTomlPath, []byte(appToml), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load app config")
		assert.Empty(t, remedy)
	})

	t.Run("returns parse error with invalid Procfile", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Invalid Procfile format (missing colon)
		procfileContent := "web missing colon here\n"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		assert.Error(t, err)
		// When Procfile parsing fails, it returns the parse error
		assert.Contains(t, err.Error(), "failed to parse Procfile")
		assert.Empty(t, remedy)
	})

	t.Run("handles relative paths correctly", func(t *testing.T) {
		// Create a temp dir and change to it
		tmpDir := t.TempDir()
		originalDir, err := os.Getwd()
		require.NoError(t, err)
		defer os.Chdir(originalDir)

		err = os.Chdir(tmpDir)
		require.NoError(t, err)

		// Create Procfile with web service (with newline at end)
		procfileContent := "web: python app.py\n"
		err = os.WriteFile("Procfile", []byte(procfileContent), 0644)
		require.NoError(t, err)

		// Test with relative path "."
		remedy, err := CheckDeployAllowed(".")
		assert.NoError(t, err)
		assert.Empty(t, remedy)
	})
}
