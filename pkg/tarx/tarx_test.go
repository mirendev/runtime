package tarx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeTar(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string // filename -> content
		dockerignore string
		includes     []string
		expected     []string // files that should be in the tar
	}{
		{
			name: "no dockerignore",
			files: map[string]string{
				"file1.txt":    "content1",
				"file2.txt":    "content2",
				"dir/file3.go": "package main",
			},
			expected: []string{"file1.txt", "file2.txt", "dir", "dir/file3.go"},
		},
		{
			name: "dockerignore specific files",
			files: map[string]string{
				"file1.txt":    "content1",
				"file2.txt":    "content2",
				"ignore.txt":   "ignored",
				"dir/file3.go": "package main",
			},
			dockerignore: "ignore.txt\n",
			expected:     []string{"file1.txt", "file2.txt", "dir", "dir/file3.go"},
		},
		{
			name: "dockerignore with patterns",
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
			dockerignore: "**/*.log\nbuild\ntemp\n",
			expected:     []string{"file1.txt", "dir", "dir/file3.go"},
		},
		{
			name: "dockerignore with comments and empty lines",
			files: map[string]string{
				"file1.txt":    "content1",
				"ignore.txt":   "ignored",
				"keep.txt":     "keep this",
				"dir/file3.go": "package main",
			},
			dockerignore: "# This is a comment\n\nignore.txt\n# Another comment\n\n",
			expected:     []string{"file1.txt", "keep.txt", "dir", "dir/file3.go"},
		},
		{
			name: "dockerignore directory exclusion",
			files: map[string]string{
				"file1.txt":                 "content1",
				"node_modules/lib.js":       "library",
				"node_modules/package.json": "package",
				"src/main.go":               "package main",
				"src/util.go":               "package main",
			},
			dockerignore: "node_modules\n",
			expected:     []string{"file1.txt", "src", "src/main.go", "src/util.go"},
		},
		{
			name: "dockerignore glob patterns",
			files: map[string]string{
				"file1.txt":     "content1",
				"test.tmp":      "temp",
				"cache.tmp":     "cache",
				"important.bak": "backup",
				"dir/file.tmp":  "temp in dir",
				"dir/keep.txt":  "keep this",
			},
			dockerignore: "**/*.tmp\n*.bak\n",
			expected:     []string{"file1.txt", "dir", "dir/keep.txt"},
		},
		{
			name: "root dockerignore targets nested files",
			files: map[string]string{
				"file1.txt":                     "content1",
				"web/index.html":                "html",
				"web/app.js":                    "js",
				"web/node_modules/lib.js":       "library",
				"web/node_modules/package.json": "package",
			},
			dockerignore: "web/node_modules\n",
			expected:     []string{"file1.txt", "web", "web/index.html", "web/app.js"},
		},
		{
			name: "multiple nested exclusions",
			files: map[string]string{
				"file1.txt":               "content1",
				"web/index.html":          "html",
				"web/node_modules/lib.js": "library",
				"api/main.go":             "package main",
				"api/vendor/dep.go":       "dependency",
			},
			dockerignore: "web/node_modules\napi/vendor\n",
			expected:     []string{"file1.txt", "web", "web/index.html", "api", "api/main.go"},
		},
		{
			name: "dockerignore scoped paths",
			files: map[string]string{
				"web/style.css": "web css",
				"web/app.js":    "js",
				"api/style.css": "api css",
				"api/main.go":   "package main",
			},
			dockerignore: "web/*.css\n",
			expected:     []string{"web", "web/app.js", "api", "api/style.css", "api/main.go"},
		},
		{
			name: "Docker patterns without double star are rooted",
			files: map[string]string{
				"debug.log":     "ignored",
				"dir/debug.log": "kept",
			},
			dockerignore: "*.log\n",
			expected:     []string{"dir", "dir/debug.log"},
		},
		{
			name: "deny all retains build inputs and a nested exception",
			files: map[string]string{
				"Dockerfile":       "FROM scratch\nCOPY dir/keep.txt /keep.txt\n",
				"Dockerfile.miren": "FROM scratch\n",
				"Procfile":         "web: ./server\n",
				".miren/app.toml":  "name = 'test'\n",
				".miren/local.key": "ignored",
				"secret.txt":       "ignored",
				"dir/keep.txt":     "kept",
				"dir/other.txt":    "ignored",
			},
			dockerignore: "*\n!dir/keep.txt\n",
			expected:     []string{"Dockerfile", "Dockerfile.miren", "Procfile", ".miren", ".miren/app.toml", "dir", "dir/keep.txt"},
		},
		{
			name: "dotfile rule retains app config but not other miren files",
			files: map[string]string{
				".miren/app.toml":  "name = 'test'\n",
				".miren/local.key": "ignored",
				".env":             "ignored",
			},
			dockerignore: ".*\n",
			expected:     []string{".miren", ".miren/app.toml"},
		},
		{
			name:         "gitignore and nested dockerignore do not filter uploads",
			dockerignore: "# Root dockerignore takes precedence\n",
			files: map[string]string{
				"file1.txt":         "content1",
				".gitignore":        "file1.txt\nweb\n",
				"web/.gitignore":    "index.html\n",
				"web/.dockerignore": "index.html\n",
				"web/index.html":    "html",
			},
			expected: []string{"file1.txt", ".gitignore", "web", "web/.gitignore", "web/.dockerignore", "web/index.html"},
		},
		{
			name: "fallback honors root and scoped nested gitignores",
			files: map[string]string{
				".gitignore":     "*.log\n",
				"debug.log":      "ignored at root",
				"web/.gitignore": "*.css\n",
				"web/debug.log":  "ignored by root rule",
				"web/style.css":  "ignored by nested rule",
				"web/app.js":     "kept",
				"api/style.css":  "kept in sibling",
			},
			expected: []string{"web", "web/app.js", "api", "api/style.css"},
		},
		{
			name: "unrelated include does not activate rules inside ignored directories",
			files: map[string]string{
				".gitignore":              "node_modules\nkeep.txt\n",
				"keep.txt":                "explicitly included",
				"node_modules/.gitignore": "!secret.txt\n",
				"node_modules/secret.txt": "must stay excluded",
			},
			includes: []string{"keep.txt"},
			expected: []string{"keep.txt"},
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

			expected := append([]string(nil), tt.expected...)
			if tt.dockerignore != "" {
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte(tt.dockerignore), 0644))
				expected = append(expected, ".dockerignore")
			}

			// Create tar
			reader, err := MakeTar(tmpDir, tt.includes, nil)
			require.NoError(t, err)

			// Extract and verify contents
			entries := extractTarEntries(t, reader)

			require.ElementsMatch(t, expected, entries, "tar entries should match expected files")
			manifest, err := ComputeManifest(tmpDir, tt.includes)
			require.NoError(t, err)
			var expectedFiles, manifestFiles []string
			for _, path := range expected {
				if _, isFile := tt.files[path]; isFile || path == ".dockerignore" {
					expectedFiles = append(expectedFiles, path)
				}
			}
			for _, entry := range manifest {
				manifestFiles = append(manifestFiles, entry.Path)
			}
			require.ElementsMatch(t, expectedFiles, manifestFiles)
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
	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)

	// Extract and verify all files are included
	entries := extractTarEntries(t, reader)
	expected := []string{"file1.txt", "file2.txt", "dir", "dir/file3.go"}
	require.ElementsMatch(t, expected, entries)
}

// A secondary jj workspace carries .jj/ with no .gitignore of its own, so the
// exclusion has to come from tarx rather than from jj's ignore file.
func TestMakeTarExcludesVCSDirs(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"main.go":                     "package main",
		".git/HEAD":                   "ref: refs/heads/main",
		".jj/repo":                    "../../main/.jj/repo",
		".jj/working_copy/tree_state": "state",
	}
	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"main.go"}, extractTarEntries(t, reader))

	manifest, err := ComputeManifest(tmpDir, nil)
	require.NoError(t, err)
	require.Len(t, manifest, 1)
	require.Equal(t, "main.go", manifest[0].Path)
}

func TestMakeTarEmptyDirectory(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "tarx-test-empty-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create tar of empty directory
	reader, err := MakeTar(tmpDir, nil, nil)
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
	reader, err := MakeTar(tmpDir, nil, nil)
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

func TestMakeTarDockerignoreNegation(t *testing.T) {
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

	// Create .dockerignore with negation pattern
	dockerignore := "**/*.log\n!important.log\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte(dockerignore), 0644))

	// Create tar
	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)

	// Extract and verify only important.log and regular.txt are included.
	// dir/ is not included because all files inside it are ignored.
	entries := extractTarEntries(t, reader)
	expected := []string{".dockerignore", "important.log", "regular.txt"}
	require.ElementsMatch(t, expected, entries)
}

// TestMakeTarBridgetownTmpPids mirrors the on-disk shape of a vanilla
// `bridgetown new` checkout: only tmp/pids/.keep exists (no tmp/.keep), with
// the dockerignore using `/tmp/*` plus a `!/tmp/pids/` negation to keep the
// pidfile directory tracked. Without the kept .keep file surviving the
// walker, lazy directory emission drops tmp/ from the tar entirely and
// Puma fails to write tmp/pids/server.pid at runtime.
func TestMakeTarBridgetownTmpPids(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-test-bridgetown-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	files := map[string]string{
		"Gemfile":         "source 'https://rubygems.org'\n",
		"config/puma.rb":  "pidfile 'tmp/pids/server.pid'\n",
		"tmp/pids/.keep":  "",
		"tmp/cache/x.txt": "should be ignored",
	}
	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	dockerignore := "/tmp/*\n!/tmp/.keep\n/tmp/pids/*\n!/tmp/pids/\n!/tmp/pids/.keep\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte(dockerignore), 0644))

	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)

	entries := extractTarEntries(t, reader)
	require.Contains(t, entries, "tmp/pids/.keep",
		"tmp/pids/.keep must be in the tar so Puma can write tmp/pids/server.pid at runtime")
	require.Contains(t, entries, "tmp",
		"tmp/ directory header must be in the tar so /app/tmp/ exists in the runtime image")
	require.NotContains(t, entries, "tmp/cache/x.txt")
}

func TestMakeTarDockerignoreRootScoping(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-sibling-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	files := map[string]string{
		".dockerignore":     "subdirA/keep.txt\n",
		"subdirA/keep.txt":  "should be ignored in subdirA",
		"subdirA/other.txt": "kept",
		"subdirB/keep.txt":  "should NOT be ignored in subdirB",
		"subdirB/other.txt": "kept",
	}
	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)

	entries := extractTarEntries(t, reader)
	require.NotContains(t, entries, "subdirA/keep.txt",
		"root .dockerignore must hide subdirA/keep.txt")
	require.Contains(t, entries, "subdirB/keep.txt",
		"scoped rule must NOT affect subdirB")
	require.Contains(t, entries, "subdirA/other.txt")
	require.Contains(t, entries, "subdirB/other.txt")
}

func TestComputeManifest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-manifest-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	files := map[string]string{
		"main.go":       "package main",
		"go.mod":        "module test",
		"lib/util.go":   "package lib",
		"ignored.log":   "log data",
		".git/HEAD":     "ref: refs/heads/main",
		"dist/build.js": "built",
	}

	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		dir := filepath.Dir(fullPath)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Dockerignore excludes *.log and dist
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte("*.log\ndist\n"), 0644))

	manifest, err := ComputeManifest(tmpDir, nil)
	require.NoError(t, err)

	// Should include main.go, go.mod, lib/util.go and .dockerignore.
	paths := make(map[string]bool)
	for _, m := range manifest {
		paths[m.Path] = true
		require.NotEmpty(t, m.Hash, "hash should be set for %s", m.Path)
		require.True(t, m.Size > 0, "size should be positive for %s", m.Path)
		require.True(t, m.Mode > 0, "mode should be set for %s", m.Path)
	}

	require.True(t, paths["main.go"])
	require.True(t, paths["go.mod"])
	require.True(t, paths["lib/util.go"])
	require.True(t, paths[".dockerignore"])
	require.False(t, paths["ignored.log"])
	require.False(t, paths[".git/HEAD"])
	require.False(t, paths["dist/build.js"])
}

func TestComputeManifestDeterministic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-manifest-det-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644))

	m1, err := ComputeManifest(tmpDir, nil)
	require.NoError(t, err)

	m2, err := ComputeManifest(tmpDir, nil)
	require.NoError(t, err)

	require.Equal(t, len(m1), len(m2))
	require.Equal(t, m1[0].Hash, m2[0].Hash)
}

func TestMakeFilteredTar(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-filtered-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	files := map[string]string{
		"main.go":     "package main",
		"go.mod":      "module test",
		"lib/util.go": "package lib",
		"lib/db.go":   "package lib",
	}

	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		dir := filepath.Dir(fullPath)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Only include main.go and lib/db.go
	onlyPaths := map[string]bool{
		"main.go":   true,
		"lib/db.go": true,
	}

	reader, err := MakeFilteredTar(tmpDir, nil, onlyPaths, nil)
	require.NoError(t, err)

	entries := extractTarEntries(t, reader)

	// Should contain the two requested files and the lib directory
	require.Contains(t, entries, "main.go")
	require.Contains(t, entries, "lib/db.go")
	require.Contains(t, entries, "lib")

	// Should NOT contain the excluded files
	require.NotContains(t, entries, "go.mod")
	require.NotContains(t, entries, "lib/util.go")
}

func TestMakeFilteredTarEmpty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-filtered-empty-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644))

	// Empty only-paths set => no files in tar
	reader, err := MakeFilteredTar(tmpDir, nil, map[string]bool{}, nil)
	require.NoError(t, err)

	entries := extractTarEntries(t, reader)
	require.Empty(t, entries)
}

func TestMakeTarOnlyIncludesDirectoriesWithAcceptedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-dir-filter-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	files := map[string]string{
		"main.go":          "package main",
		"lib/util.go":      "package lib",
		"empty/readme.txt": "hello",
		"deep/a/b/file.go": "package b",
	}

	for filename, content := range files {
		fullPath := filepath.Join(tmpDir, filename)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	t.Run("unfiltered includes all directories", func(t *testing.T) {
		reader, err := MakeTar(tmpDir, nil, nil)
		require.NoError(t, err)

		entries := extractTarEntries(t, reader)
		require.Contains(t, entries, "lib")
		require.Contains(t, entries, "empty")
		require.Contains(t, entries, "deep")
		require.Contains(t, entries, "deep/a")
		require.Contains(t, entries, "deep/a/b")
	})

	t.Run("filtered excludes directories with no accepted files", func(t *testing.T) {
		onlyPaths := map[string]bool{
			"main.go":     true,
			"lib/util.go": true,
		}

		reader, err := MakeFilteredTar(tmpDir, nil, onlyPaths, nil)
		require.NoError(t, err)

		entries := extractTarEntries(t, reader)
		require.Contains(t, entries, "main.go")
		require.Contains(t, entries, "lib")
		require.Contains(t, entries, "lib/util.go")

		// Directories with no accepted files should not appear
		require.NotContains(t, entries, "empty")
		require.NotContains(t, entries, "deep")
		require.NotContains(t, entries, "deep/a")
		require.NotContains(t, entries, "deep/a/b")
	})

	t.Run("filtered emits nested parent directories", func(t *testing.T) {
		onlyPaths := map[string]bool{
			"deep/a/b/file.go": true,
		}

		reader, err := MakeFilteredTar(tmpDir, nil, onlyPaths, nil)
		require.NoError(t, err)

		entries := extractTarEntries(t, reader)
		require.ElementsMatch(t, []string{"deep", "deep/a", "deep/a/b", "deep/a/b/file.go"}, entries)
	})
}

func TestTarFS_PreExistingDirectory(t *testing.T) {
	dir := t.TempDir()

	// Pre-create directories that also appear in the tar, simulating the
	// delta deploy case where stageMatchingFiles already created them.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "db", "migrations"), 0755))

	// Build a gzipped tar containing the same directory entries plus a file.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "db/", Typeflag: tar.TypeDir, Mode: 0755}))
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "db/migrations/", Typeflag: tar.TypeDir, Mode: 0755}))

	content := []byte("CREATE TABLE test;")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "db/migrations/001_init.sql",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	// TarFS should succeed despite the directories already existing.
	_, err = TarFS(&buf, dir)
	require.NoError(t, err, "TarFS should tolerate pre-existing directories")

	got, err := os.ReadFile(filepath.Join(dir, "db", "migrations", "001_init.sql"))
	require.NoError(t, err)
	require.Equal(t, string(content), string(got))
}

func TestTarFSRejectsEscapingPaths(t *testing.T) {
	tests := []struct {
		name        string
		headerName  func(parent string) string
		outsidePath func(parent string) string
	}{
		{
			name: "parent traversal",
			headerName: func(string) string {
				return "../outside"
			},
			outsidePath: func(parent string) string {
				return filepath.Join(parent, "outside")
			},
		},
		{
			name: "embedded traversal",
			headerName: func(string) string {
				return "nested/../../outside"
			},
			outsidePath: func(parent string) string {
				return filepath.Join(parent, "outside")
			},
		},
		{
			name: "sibling prefix",
			headerName: func(string) string {
				return "../dest-evil/outside"
			},
			outsidePath: func(parent string) string {
				return filepath.Join(parent, "dest-evil", "outside")
			},
		},
		{
			name: "absolute path",
			headerName: func(parent string) string {
				return filepath.Join(parent, "outside")
			},
			outsidePath: func(parent string) string {
				return filepath.Join(parent, "outside")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "dest")
			require.NoError(t, os.Mkdir(dest, 0755))

			content := []byte("escaped")
			var buf bytes.Buffer
			gw := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gw)
			require.NoError(t, tw.WriteHeader(&tar.Header{
				Name:     tt.headerName(parent),
				Typeflag: tar.TypeReg,
				Mode:     0644,
				Size:     int64(len(content)),
			}))
			_, err := tw.Write(content)
			require.NoError(t, err)
			require.NoError(t, tw.Close())
			require.NoError(t, gw.Close())

			_, err = TarFS(&buf, dest)
			require.ErrorContains(t, err, "archive path")
			_, err = os.Stat(tt.outsidePath(parent))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestTarFSRejectsPreExistingSymlinkTraversal(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	outside := filepath.Join(parent, "outside")
	require.NoError(t, os.Mkdir(dest, 0755))
	require.NoError(t, os.Mkdir(outside, 0755))
	require.NoError(t, os.Symlink(outside, filepath.Join(dest, "escape")))

	content := []byte("escaped")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "escape/file",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	_, err = TarFS(&buf, dest)
	require.ErrorContains(t, err, "traverses symlink")
	_, err = os.Stat(filepath.Join(outside, "file"))
	require.ErrorIs(t, err, os.ErrNotExist)
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

	// Create .dockerignore that would normally exclude dist and node_modules
	dockerignore := "dist\nnode_modules\nbuild\n*.log\n**/*.generated\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte(dockerignore), 0644))

	// Test with include patterns that override dockerignore
	// Using gitignore-style patterns including the ** pattern
	includePatterns := []string{"dist", "dist/**", "*.log", "**/*.generated"}
	reader, err := MakeTar(tmpDir, includePatterns, nil)
	require.NoError(t, err)

	// Extract and verify dist files and log files are included despite dockerignore
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

func TestBuildContextPolicy(t *testing.T) {
	for _, tt := range []struct {
		name                string
		dockerignore        string
		dockerignorePresent bool
		includes            []string
		config              bool
		secret              bool
		localConfig         bool
	}{
		{name: "absent dockerignore preserves gitignore filtering"},
		{name: "empty dockerignore overrides gitignore", dockerignorePresent: true, config: true, secret: true},
		{name: "fallback leaf include does not bypass an ignored parent", includes: []string{"config/rubygems.yml"}},
		{name: "fallback directory include overrides gitignore", includes: []string{"config", ".git/**", ".jj/**"}, config: true, localConfig: true},
		{name: "dockerignore excludes tracked files too", dockerignore: "config\n.env\n"},
		{name: "include reaches a file under an ignored directory", dockerignore: "config\n.env\n", includes: []string{"config/rubygems.yml", ".git/**", ".jj/**"}, config: true},
		{name: "negation reaches a file under an ignored directory", dockerignore: "config\n!config/rubygems.yml\n.env\n", config: true},
		{name: "last matching rule wins", dockerignore: "config\n!config/rubygems.yml\nconfig/rubygems.yml\n.env\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			output, err := exec.Command("git", "init", dir).CombinedOutput()
			require.NoError(t, err, "%s", output)
			files := map[string]string{
				"config/rubygems.yml": "production: {}\n",
				"config/local.yml":    "local: {}\n",
				".env":                "LOCAL_SECRET=example\n",
				".jj/state":           "private metadata",
			}
			for path, data := range files {
				full := filepath.Join(dir, path)
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
				require.NoError(t, os.WriteFile(full, []byte(data), 0644))
			}
			output, err = exec.Command("git", "-C", dir, "add", "config/rubygems.yml").CombinedOutput()
			require.NoError(t, err, "%s", output)
			// Add the ignore rules after tracking the file, as in MIR-1459.
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("config\n.env\n"), 0644))
			if tt.dockerignore != "" || tt.dockerignorePresent {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte(tt.dockerignore), 0644))
			}
			var expected []string
			if tt.dockerignore != "" || tt.dockerignorePresent {
				expected = append(expected, ".gitignore", ".dockerignore")
			}
			if tt.config {
				expected = append(expected, "config/rubygems.yml")
			}
			if tt.secret {
				expected = append(expected, ".env")
			}
			if tt.secret || tt.localConfig {
				expected = append(expected, "config/local.yml")
			}

			manifest, err := ComputeManifest(dir, tt.includes)
			require.NoError(t, err)
			var paths []string
			for _, entry := range manifest {
				paths = append(paths, entry.Path)
			}
			require.ElementsMatch(t, expected, paths)

			for _, filtered := range []bool{false, true} {
				var reader io.ReadCloser
				if filtered {
					onlyPaths := map[string]bool{}
					// Deliberately request excluded files too: filtering must
					// not bypass the build-context policy for delta uploads.
					for path := range files {
						onlyPaths[path] = true
					}
					onlyPaths[".gitignore"] = true
					onlyPaths[".dockerignore"] = true
					reader, err = MakeFilteredTar(dir, tt.includes, onlyPaths, nil)
				} else {
					reader, err = MakeTar(dir, tt.includes, nil)
				}
				require.NoError(t, err)
				gz, err := gzip.NewReader(reader)
				require.NoError(t, err)
				contents, err := TarToMap(gz)
				require.NoError(t, err)
				require.NoError(t, gz.Close())
				require.NoError(t, reader.Close())
				var tarPaths []string
				for path := range contents {
					tarPaths = append(tarPaths, path)
				}
				require.ElementsMatch(t, expected, tarPaths)
				if tt.config {
					require.Equal(t, files["config/rubygems.yml"], string(contents["config/rubygems.yml"]))
				}
			}
		})
	}
}

func TestDockerignoreErrors(t *testing.T) {
	for _, directory := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, ".dockerignore")
		if directory {
			require.NoError(t, os.Mkdir(path, 0755))
		} else {
			require.NoError(t, os.WriteFile(path, []byte("[\n"), 0644))
		}
		_, err := ComputeManifest(dir, nil)
		require.ErrorContains(t, err, ".dockerignore")
		_, err = MakeTar(dir, nil, nil)
		require.ErrorContains(t, err, ".dockerignore")
	}
}

func TestWriteFileContent(t *testing.T) {
	t.Run("exact size copies everything", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, writeFileContent(&out, bytes.NewReader([]byte("hello")), 5))
		require.Equal(t, "hello", out.String())
	})

	t.Run("file that grew is cut at the promised size", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, writeFileContent(&out, bytes.NewReader([]byte("hello world")), 5))
		require.Equal(t, "hello", out.String())
	})

	t.Run("file that shrank is padded with zeros", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, writeFileContent(&out, bytes.NewReader([]byte("hi")), 5))
		require.Equal(t, "hi\x00\x00\x00", out.String())
	})
}

// TestMakeTar_FileGrowsDuringArchive is the real-world shape: a file inside the
// project is appended to while the archive is being written (a log, or the
// deploy's own redirected output). The archive must still be well-formed and
// carry the file at the size its header declared.
func TestMakeTar_FileGrowsDuringArchive(t *testing.T) {
	dir := t.TempDir()
	growing := filepath.Join(dir, "out.log")
	require.NoError(t, os.WriteFile(growing, []byte("line one\n"), 0o644))

	f, err := os.OpenFile(growing, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	defer f.Close()

	// A reader that appends to the file every time the archive is read from,
	// so the file is guaranteed to be larger by the time its content is copied.
	tarReader, err := MakeTar(dir, nil, nil)
	require.NoError(t, err)
	defer tarReader.Close()

	var buf bytes.Buffer
	chunk := make([]byte, 1)
	for {
		n, rerr := tarReader.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			_, _ = f.WriteString("more\n")
		}
		if rerr == io.EOF {
			break
		}
		require.NoError(t, rerr)
	}

	gz, err := gzip.NewReader(&buf)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if hdr.Name != "out.log" {
			continue
		}
		found = true
		content, err := io.ReadAll(tr)
		require.NoError(t, err)
		require.Equal(t, hdr.Size, int64(len(content)))
		require.Equal(t, "line one\n", string(content))
	}
	require.True(t, found, "out.log missing from archive")
}
