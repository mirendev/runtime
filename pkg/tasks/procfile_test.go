package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantProcs   []Proc
		wantErr     bool
		errContains string
	}{
		{
			name: "basic procfile with newlines",
			content: `web: npm start
worker: npm run worker
`,
			wantProcs: []Proc{
				{Name: "web", Command: []string{"sh", "-c", "npm start"}},
				{Name: "worker", Command: []string{"sh", "-c", "npm run worker"}},
			},
		},
		{
			name: "procfile without ending newline",
			content: `web: npm start
worker: npm run worker`,
			wantProcs: []Proc{
				{Name: "web", Command: []string{"sh", "-c", "npm start"}},
				{Name: "worker", Command: []string{"sh", "-c", "npm run worker"}},
			},
		},
		{
			name:    "single line without newline",
			content: `web: npm start`,
			wantProcs: []Proc{
				{Name: "web", Command: []string{"sh", "-c", "npm start"}},
			},
		},
		{
			name: "procfile with extra spaces",
			content: `web:    npm start   
worker:npm run worker
`,
			wantProcs: []Proc{
				{Name: "web", Command: []string{"sh", "-c", "npm start"}},
				{Name: "worker", Command: []string{"sh", "-c", "npm run worker"}},
			},
		},
		{
			name: "procfile with complex commands",
			content: `web: bundle exec puma -C config/puma.rb
worker: bundle exec sidekiq -C config/sidekiq.yml
release: rake db:migrate
`,
			wantProcs: []Proc{
				{Name: "web", Command: []string{"sh", "-c", "bundle exec puma -C config/puma.rb"}},
				{Name: "worker", Command: []string{"sh", "-c", "bundle exec sidekiq -C config/sidekiq.yml"}},
				{Name: "release", Command: []string{"sh", "-c", "rake db:migrate"}},
			},
		},
		{
			name: "invalid line without colon",
			content: `web npm start
`,
			wantErr:     true,
			errContains: "invalid line",
		},
		{
			name:      "empty procfile",
			content:   ``,
			wantProcs: []Proc{},
		},
		{
			name: "procfile with blank lines between commands",
			content: `web: npm start

worker: npm run worker
`,
			wantProcs: []Proc{
				{Name: "web", Command: []string{"sh", "-c", "npm start"}},
				{Name: "worker", Command: []string{"sh", "-c", "npm run worker"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file with the test content
			tmpDir := t.TempDir()
			procfilePath := filepath.Join(tmpDir, "Procfile")
			err := os.WriteFile(procfilePath, []byte(tt.content), 0644)
			require.NoError(t, err)

			// Parse the file
			pf, err := ParseFile(procfilePath)

			// Check error expectations
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pf)

			// Check the parsed processes
			assert.Len(t, pf.Proceses, len(tt.wantProcs))
			for i, wantProc := range tt.wantProcs {
				if i < len(pf.Proceses) {
					assert.Equal(t, wantProc.Name, pf.Proceses[i].Name, "Process name mismatch at index %d", i)
					assert.Equal(t, wantProc.Command, pf.Proceses[i].Command, "Process command mismatch at index %d", i)
				}
			}
		})
	}
}

func TestParseFileWithMissingFile(t *testing.T) {
	_, err := ParseFile("/non/existent/path/Procfile")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no such file")
}

func TestParseFileHandlesLastLineWithoutNewline(t *testing.T) {
	// This test verifies that the parser correctly handles the last line
	// even when it doesn't end with a newline

	t.Run("two lines without final newline", func(t *testing.T) {
		tmpDir := t.TempDir()
		procfilePath := filepath.Join(tmpDir, "Procfile")

		// Write content without a trailing newline
		content := []byte("web: npm start\nworker: npm run worker")
		err := os.WriteFile(procfilePath, content, 0644)
		require.NoError(t, err)

		pf, err := ParseFile(procfilePath)
		require.NoError(t, err)
		require.NotNil(t, pf)

		// Should correctly parse both lines
		assert.Len(t, pf.Proceses, 2)
		assert.Equal(t, "web", pf.Proceses[0].Name)
		assert.Equal(t, []string{"sh", "-c", "npm start"}, pf.Proceses[0].Command)
		assert.Equal(t, "worker", pf.Proceses[1].Name)
		assert.Equal(t, []string{"sh", "-c", "npm run worker"}, pf.Proceses[1].Command)
	})

	t.Run("single line without newline", func(t *testing.T) {
		tmpDir := t.TempDir()
		procfilePath := filepath.Join(tmpDir, "Procfile")

		// Write a single line without newline
		content := []byte("web: npm start")
		err := os.WriteFile(procfilePath, content, 0644)
		require.NoError(t, err)

		pf, err := ParseFile(procfilePath)
		require.NoError(t, err)
		require.NotNil(t, pf)

		// Should correctly parse the single line
		assert.Len(t, pf.Proceses, 1)
		assert.Equal(t, "web", pf.Proceses[0].Name)
		assert.Equal(t, []string{"sh", "-c", "npm start"}, pf.Proceses[0].Command)
	})

	t.Run("three lines without final newline", func(t *testing.T) {
		tmpDir := t.TempDir()
		procfilePath := filepath.Join(tmpDir, "Procfile")

		// Write three lines without trailing newline
		content := []byte("web: npm start\nworker: npm run worker\nscheduler: npm run scheduler")
		err := os.WriteFile(procfilePath, content, 0644)
		require.NoError(t, err)

		pf, err := ParseFile(procfilePath)
		require.NoError(t, err)
		require.NotNil(t, pf)

		// Should correctly parse all three lines
		assert.Len(t, pf.Proceses, 3)
		assert.Equal(t, "web", pf.Proceses[0].Name)
		assert.Equal(t, []string{"sh", "-c", "npm start"}, pf.Proceses[0].Command)
		assert.Equal(t, "worker", pf.Proceses[1].Name)
		assert.Equal(t, []string{"sh", "-c", "npm run worker"}, pf.Proceses[1].Command)
		assert.Equal(t, "scheduler", pf.Proceses[2].Name)
		assert.Equal(t, []string{"sh", "-c", "npm run scheduler"}, pf.Proceses[2].Command)
	})
}
