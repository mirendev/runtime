package tarx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMakeTar(t *testing.T) {
	tests := []struct {
		name             string
		files            map[string]string // filename -> content
		gitignore        string
		nestedGitignores map[string]string // dir path -> content
		expected         []string          // files that should be in the tar
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
		{
			name: "nested gitignore basic",
			files: map[string]string{
				"file1.txt":                     "content1",
				"web/index.html":                "html",
				"web/app.js":                    "js",
				"web/node_modules/lib.js":       "library",
				"web/node_modules/package.json": "package",
			},
			nestedGitignores: map[string]string{
				"web": "node_modules\n",
			},
			expected: []string{"file1.txt", "web", "web/index.html", "web/app.js"},
		},
		{
			name: "multiple nested gitignores",
			files: map[string]string{
				"file1.txt":               "content1",
				"web/index.html":          "html",
				"web/node_modules/lib.js": "library",
				"api/main.go":             "package main",
				"api/vendor/dep.go":       "dependency",
			},
			nestedGitignores: map[string]string{
				"web": "node_modules\n",
				"api": "vendor\n",
			},
			expected: []string{"file1.txt", "web", "web/index.html", "api", "api/main.go"},
		},
		{
			name: "nested gitignore scoping",
			files: map[string]string{
				"web/style.css": "web css",
				"web/app.js":    "js",
				"api/style.css": "api css",
				"api/main.go":   "package main",
			},
			nestedGitignores: map[string]string{
				"web": "*.css\n",
			},
			expected: []string{"web", "web/app.js", "api", "api/style.css", "api/main.go"},
		},
		{
			name: "nested gitignore files excluded from tar",
			files: map[string]string{
				"file1.txt":      "content1",
				"web/index.html": "html",
			},
			gitignore: "*.log\n",
			nestedGitignores: map[string]string{
				"web": "*.tmp\n",
			},
			expected: []string{"file1.txt", "web", "web/index.html"},
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

			// Create nested .gitignore files
			for dir, content := range tt.nestedGitignores {
				dirPath := filepath.Join(tmpDir, dir)
				require.NoError(t, os.MkdirAll(dirPath, 0755))
				gitignorePath := filepath.Join(dirPath, ".gitignore")
				require.NoError(t, os.WriteFile(gitignorePath, []byte(content), 0644))
			}

			// Create tar
			reader, err := MakeTar(tmpDir, nil, nil)
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
	reader, err := MakeTar(tmpDir, nil, nil)
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
	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)

	// Extract and verify only important.log and regular.txt are included.
	// dir/ is not included because all files inside it are gitignored.
	entries := extractTarEntries(t, reader)
	expected := []string{"important.log", "regular.txt"}
	require.ElementsMatch(t, expected, entries)
}

// TestMakeTarBridgetownTmpPids mirrors the on-disk shape of a vanilla
// `bridgetown new` checkout: only tmp/pids/.keep exists (no tmp/.keep), with
// the gitignore using `/tmp/*` plus a `!/tmp/pids/` negation to keep the
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

	gitignore := "/tmp/*\n!/tmp/.keep\n/tmp/pids/*\n!/tmp/pids/\n!/tmp/pids/.keep\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignore), 0644))

	reader, err := MakeTar(tmpDir, nil, nil)
	require.NoError(t, err)

	entries := extractTarEntries(t, reader)
	require.Contains(t, entries, "tmp/pids/.keep",
		"tmp/pids/.keep must be in the tar so Puma can write tmp/pids/server.pid at runtime")
	require.Contains(t, entries, "tmp",
		"tmp/ directory header must be in the tar so /app/tmp/ exists in the runtime image")
	require.NotContains(t, entries, "tmp/cache/x.txt")
}

// TestMakeTarSiblingGitignoresDoNotCrossPollute verifies that when a nested
// .gitignore is encountered, its patterns do not leak to sibling directories.
// We accumulate patterns into a single slice as we walk, so this test pins
// down that go-git's per-pattern `domain` actually scopes matches to the
// gitignore's directory subtree.
func TestMakeTarSiblingGitignoresDoNotCrossPollute(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tarx-sibling-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	files := map[string]string{
		"subdirA/.gitignore": "keep.txt\n",
		"subdirA/keep.txt":   "should be ignored in subdirA",
		"subdirA/other.txt":  "kept",
		"subdirB/keep.txt":   "should NOT be ignored in subdirB",
		"subdirB/other.txt":  "kept",
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
		"subdirA/.gitignore must hide keep.txt in subdirA")
	require.Contains(t, entries, "subdirB/keep.txt",
		"subdirA/.gitignore must NOT affect subdirB")
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

	// Gitignore excludes *.log and dist
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("*.log\ndist\n"), 0644))

	manifest, err := ComputeManifest(tmpDir, nil)
	require.NoError(t, err)

	// Should include main.go, go.mod, lib/util.go (not .gitignore, not .git, not *.log, not dist)
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

	// Create .gitignore that would normally exclude dist and node_modules
	gitignore := "dist\nnode_modules\nbuild\n*.log\n*.generated\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignore), 0644))

	// Test with include patterns that override gitignore
	// Using gitignore-style patterns including the ** pattern
	includePatterns := []string{"dist", "dist/**", "*.log", "**/*.generated"}
	reader, err := MakeTar(tmpDir, includePatterns, nil)
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
