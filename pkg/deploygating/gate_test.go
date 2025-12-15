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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid app configuration")
		assert.Contains(t, remedy, "Fix the configuration error")
	})

	t.Run("returns parse error with invalid Procfile", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Invalid Procfile format (missing colon)
		procfileContent := "web missing colon here\n"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		remedy, err := CheckDeployAllowed(tmpDir)
		require.Error(t, err)
		// When Procfile parsing fails, it returns the parse error
		assert.Contains(t, err.Error(), "failed to parse Procfile")
		assert.Contains(t, remedy, "Fix the syntax error in Procfile")
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

	t.Run("handles Procfile without ending newline", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create Procfile with web service but no ending newline
		procfileContent := "web: python app.py"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		// Should succeed - the parser now handles files without ending newlines
		remedy, err := CheckDeployAllowed(tmpDir)
		assert.NoError(t, err)
		assert.Empty(t, remedy)
	})

	t.Run("handles multi-line Procfile without ending newline", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create Procfile with multiple services but no ending newline
		procfileContent := "worker: python worker.py\nweb: python app.py"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		// Should succeed - the parser now handles the last line without newline
		remedy, err := CheckDeployAllowed(tmpDir)
		assert.NoError(t, err)
		assert.Empty(t, remedy)
	})

	t.Run("returns error for Procfile with invalid syntax", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create Procfile with invalid line (no colon separator)
		procfileContent := "web python app.py\nworker: python worker.py"
		procfilePath := filepath.Join(tmpDir, "Procfile")
		err := os.WriteFile(procfilePath, []byte(procfileContent), 0644)
		require.NoError(t, err)

		// Should fail with parse error
		remedy, err := CheckDeployAllowed(tmpDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Procfile")
		assert.Contains(t, err.Error(), "invalid line")
		assert.Contains(t, remedy, "Fix the syntax error in Procfile")
	})
}
