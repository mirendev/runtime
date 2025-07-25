package tarx

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeTar(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string // filename -> content
		gitignore string
		expected  []string // files that should be in the tar
	}{
		{
			name: "no gitignore",
			files: map[string]string{
				"file1.txt":    "content1",
				"file2.txt":    "content2",
				"dir/file3.go": "package main",
			},
			expected: []string{"file1.txt", "file2.txt", "dir", "dir/file3.go"},
		},
		{
			name: "gitignore specific files",
			files: map[string]string{
				"file1.txt":    "content1",
				"file2.txt":    "content2",
				"ignore.txt":   "ignored",
				"dir/file3.go": "package main",
			},
			gitignore: "ignore.txt\n",
			expected:  []string{"file1.txt", "file2.txt", "dir", "dir/file3.go"},
		},
		{
			name: "gitignore with patterns",
			files: map[string]string{
				"file1.txt":      "content1",
				"file2.log":      "log content",
				"debug.log":      "debug content",
				"dir/app.log":    "app log",
				"dir/file3.go":   "package main",
				"build/output.o": "binary",
				"build/main.exe": "executable",
				"temp/cache.tmp": "temp file",
			},
			gitignore: "*.log\nbuild\ntemp\n",
			expected:  []string{"file1.txt", "dir", "dir/file3.go"},
		},
		{
			name: "gitignore with comments and empty lines",
			files: map[string]string{
				"file1.txt":    "content1",
				"ignore.txt":   "ignored",
				"keep.txt":     "keep this",
				"dir/file3.go": "package main",
			},
			gitignore: "# This is a comment\n\nignore.txt\n# Another comment\n\n",
			expected:  []string{"file1.txt", "keep.txt", "dir", "dir/file3.go"},
		},
		{
			name: "gitignore directory exclusion",
			files: map[string]string{
				"file1.txt":                 "content1",
				"node_modules/lib.js":       "library",
				"node_modules/package.json": "package",
				"src/main.go":               "package main",
				"src/util.go":               "package main",
			},
			gitignore: "node_modules\n",
			expected:  []string{"file1.txt", "src", "src/main.go", "src/util.go"},
		},
		{
			name: "gitignore glob patterns",
			files: map[string]string{
				"file1.txt":     "content1",
				"test.tmp":      "temp",
				"cache.tmp":     "cache",
				"important.bak": "backup",
				"dir/file.tmp":  "temp in dir",
				"dir/keep.txt":  "keep this",
			},
			gitignore: "*.tmp\n*.bak\n",
			expected:  []string{"file1.txt", "dir", "dir/keep.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir, err := os.MkdirTemp("", "tarx-test-")
			require.NoError(t, err)
			defer os.RemoveAll(tmpDir)

			// Create test files
			for filename, content := range tt.files {
				fullPath := filepath.Join(tmpDir, filename)
				dir := filepath.Dir(fullPath)
				require.NoError(t, os.MkdirAll(dir, 0755))
				require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
			}

			// Create .gitignore if specified
			if tt.gitignore != "" {
				gitignorePath := filepath.Join(tmpDir, ".gitignore")
				require.NoError(t, os.WriteFile(gitignorePath, []byte(tt.gitignore), 0644))
			}

			// Create tar
			reader, err := MakeTar(tmpDir, nil)
			require.NoError(t, err)

			// Extract and verify contents
			entries := extractTarEntries(t, reader)

			require.ElementsMatch(t, tt.expected, entries, "tar entries should match expected files")
		})
	}
}

func TestMakeTarWithoutGitignore(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "tarx-test-no-gitignore-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files
	files := map[string]string{
		"file1.txt":    "content1",
		"file2.txt":    "content2",
		"dir/file3.go": "package main",
	}

	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		dir := filepath.Dir(fullPath)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Create tar (no .gitignore file)
	reader, err := MakeTar(tmpDir, nil)
	require.NoError(t, err)

	// Extract and verify all files are included
	entries := extractTarEntries(t, reader)
	expected := []string{"file1.txt", "file2.txt", "dir", "dir/file3.go"}
	require.ElementsMatch(t, expected, entries)
}

func TestMakeTarEmptyDirectory(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "tarx-test-empty-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create tar of empty directory
	reader, err := MakeTar(tmpDir, nil)
	require.NoError(t, err)

	// Verify no entries
	entries := extractTarEntries(t, reader)
	require.Empty(t, entries)
}

// Helper function to extract tar entries and return their names
func extractTarEntries(t *testing.T, reader io.Reader) []string {
	gzr, err := gzip.NewReader(reader)
	require.NoError(t, err)
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var entries []string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		entries = append(entries, hdr.Name)

		// Skip file content
		if hdr.Typeflag == tar.TypeReg {
			_, err := io.Copy(io.Discard, tr)
			require.NoError(t, err)
		}
	}

	return entries
}

func TestMakeTarVerifyContent(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "tarx-test-content-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files with specific content
	testContent := "Hello, World!"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte(testContent), 0644))

	// Create tar
	reader, err := MakeTar(tmpDir, nil)
	require.NoError(t, err)

	// Extract and verify content
	gzr, err := gzip.NewReader(reader)
	require.NoError(t, err)
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	hdr, err := tr.Next()
	require.NoError(t, err)
	require.Equal(t, "test.txt", hdr.Name)

	content, err := io.ReadAll(tr)
	require.NoError(t, err)
	require.Equal(t, testContent, string(content))
}

func TestMakeTarGitignoreNegation(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "tarx-test-negation-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files
	files := map[string]string{
		"file1.log":     "log1",
		"file2.log":     "log2",
		"important.log": "important log",
		"dir/debug.log": "debug",
		"dir/error.log": "error",
		"regular.txt":   "text",
	}

	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		dir := filepath.Dir(fullPath)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Create .gitignore with negation pattern
	gitignore := "*.log\n!important.log\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignore), 0644))

	// Create tar
	reader, err := MakeTar(tmpDir, nil)
	require.NoError(t, err)

	// Extract and verify only important.log and regular.txt are included
	entries := extractTarEntries(t, reader)
	expected := []string{"important.log", "regular.txt", "dir"}
	require.ElementsMatch(t, expected, entries)
}

func TestMakeTarWithIncludePatterns(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "tarx-test-include-")
	require.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Create test files
	files := map[string]string{
		"file1.txt":                       "content1",
		"file2.log":                       "log content",
		"dist/bundle.js":                  "bundled js",
		"dist/styles.css":                 "styles",
		"node_modules/lib.js":             "library",
		"build/output.o":                  "binary",
		"src/main.go":                     "package main",
		"src/generated/api.generated":     "generated api",
		"test/nested/deep/file.generated": "deep generated file",
	}

	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		dir := filepath.Dir(fullPath)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Create .gitignore that would normally exclude dist and node_modules
	gitignore := "dist\nnode_modules\nbuild\n*.log\n*.generated\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignore), 0644))

	// Test with include patterns that override gitignore
	// Using gitignore-style patterns including the ** pattern
	includePatterns := []string{"dist", "dist/**", "*.log", "**/*.generated"}
	reader, err := MakeTar(tmpDir, includePatterns)
	require.NoError(t, err)

	// Extract and verify dist files and log files are included despite gitignore
	entries := extractTarEntries(t, reader)

	// These should be included
	expectedIncluded := []string{
		"dist", "dist/bundle.js", "dist/styles.css",
		"file2.log",
		"src", "src/generated", "src/generated/api.generated",
		"test", "test/nested", "test/nested/deep", "test/nested/deep/file.generated",
	}
	for _, expected := range expectedIncluded {
		require.Contains(t, entries, expected, "file %s should be included", expected)
	}

	// These should still be excluded
	notExpected := []string{"node_modules", "node_modules/lib.js", "build", "build/output.o"}
	for _, notExp := range notExpected {
		require.NotContains(t, entries, notExp, "file %s should be excluded", notExp)
	}
}
