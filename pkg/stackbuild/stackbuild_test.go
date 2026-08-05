package stackbuild

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/tarx"

	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
)

// helper function to execute LLB locally
func buildLLB(t *testing.T, dir string, state *llb.State, check ...func(f io.Reader)) {
	t.Helper()
	ctx := context.Background()

	cl, err := client.NewClientWithOpts(client.FromEnv)
	require.NoError(t, err)

	// Pull buildkit image
	pullReader, err := cl.ImagePull(ctx, imagerefs.BuildKit, image.PullOptions{})
	require.NoError(t, err)
	defer func() {
		if err := pullReader.Close(); err != nil {
			t.Logf("failed to close pull reader: %v", err)
		}
	}()

	// Read the pull output to ensure the image is fully pulled
	_, err = io.Copy(io.Discard, pullReader)
	require.NoError(t, err)

	// Create buildkit container
	resp, err := cl.ContainerCreate(ctx,
		&container.Config{
			Image: imagerefs.BuildKit,
		},
		&container.HostConfig{
			Privileged: true,
		},
		&network.NetworkingConfig{},
		nil,
		"",
	)
	require.NoError(t, err)

	defer func() {
		err := cl.ContainerKill(ctx, resp.ID, "KILL")
		if err != nil {
			t.Logf("failed to kill container: %v", err)
		}
		err = cl.ContainerRemove(ctx, resp.ID, container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		})
		if err != nil {
			t.Logf("failed to remove container: %v", err)
		}
	}()

	var buf bytes.Buffer

	go func() {
		r, err := cl.ContainerLogs(ctx, resp.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if err != nil {
			t.Logf("failed to get container logs: %v", err)
		}
		defer r.Close()

		io.Copy(&buf, r)
	}()

	err = cl.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	c, err := buildkit.New(ctx, "docker-container://"+resp.ID)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Info(ctx)
	require.NoError(t, err)

	def, err := state.Marshal(ctx)
	require.NoError(t, err)

	pw, err := progresswriter.NewPrinter(ctx, os.Stdout, "plain")
	require.NoError(t, err)

	f, err := os.CreateTemp(t.TempDir(), "buildkit-llb")
	require.NoError(t, err)

	defer f.Close()

	cfg, err := config.Load(config.Dir())
	require.NoError(t, err)

	da := authprovider.NewDockerAuthProvider(cfg, nil)

	_, err = c.Solve(ctx, def, buildkit.SolveOpt{
		Session: []session.Attachable{
			da,
		},
		LocalDirs: map[string]string{
			"context": dir,
		},
		Exports: []buildkit.ExportEntry{
			{
				Type: buildkit.ExporterTar,
				Output: func(m map[string]string) (io.WriteCloser, error) {
					return f, nil
				},
			},
		},
		CacheExports: []buildkit.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"dest": "/tmp/test-cache",
				},
			},
		},
		CacheImports: []buildkit.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"src": "/tmp/test-cache",
				},
			},
		},
	}, pw.Status())
	require.NoError(t, err)

	f, err = os.Open(f.Name())
	require.NoError(t, err)

	for _, cf := range check {
		f.Seek(0, io.SeekStart)
		cf(f)
	}

	require.NoError(t, err)
}

func setupTestDir(root string, t *testing.T) string {
	t.Helper()
	dir := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(dir, 0755))
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile("testdata/" + path)
	require.NoError(t, err)
	return string(content)
}

func checkDocker() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

func TestRails(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	// Create minimal Rails project structure
	for _, d := range []string{"app", "config", "lib", "bin"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0755))
	}

	files := map[string]string{
		"Gemfile":               readFile(t, "rails/Gemfile"),
		"Gemfile.lock":          readFile(t, "rails/Gemfile.lock"),
		"Rakefile":              "",
		"config/routes.rb":      "Rails.application.routes.draw {}",
		"config/application.rb": "module TestApp; class Application < Rails::Application; end; end",
		"lib/blah.rb":           "",
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	os.Chmod(filepath.Join(dir, "bin/rake"), 0755)

	stack := &RubyStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "3.2"})
	require.NoError(t, err)

	buildLLB(t, dir, state)

	img := stack.Image()
	require.Equal(t, []string{"/bin/sh", "-c", "exec bundle exec rails server -b 0.0.0.0 -p $PORT"}, img.Config.Entrypoint)
}

func TestRuby(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	// Create minimal Ruby project
	files := map[string]string{
		"Gemfile":      readFile(t, "ruby/Gemfile"),
		"Gemfile.lock": readFile(t, "ruby/Gemfile.lock"),
		"app.rb":       "puts 'Hello, World!'",
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &RubyStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "3.2"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
	img := stack.Image()
	require.Equal(t, []string{"/bin/sh", "-c", "exec bundle exec puma -b tcp://0.0.0.0 -p $PORT"}, img.Config.Entrypoint)
}

func TestPython(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	// Test with requirements.txt
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "requirements.txt"),
		[]byte("requests==2.31.0"),
		0644,
	))

	stack := &PythonStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)

	// Clean up and test with Pipfile
	os.RemoveAll(dir)

	root = t.TempDir()
	dir = setupTestDir(root, t)

	files := map[string]string{
		"Pipfile":      `[[source]]\nurl = "https://pypi.org/simple"\nverify_ssl = true\nname = "pypi"\n\n[packages]\nrequests = "*"`,
		"Pipfile.lock": "{}",
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	state, err = stack.GenerateLLB(dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestPythonPoetry(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"README.md":      `test app`,
		"pyproject.toml": readFile(t, "python/pyproject.toml"),
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &PythonStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestNode(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	// Test with npm
	files := map[string]string{
		"package.json": `{
			"name": "test-app",
			"version": "1.0.0",
			"dependencies": {
				"express": "^4.18.2"
			}
		}`,
		"index.js":          "console.log('Hello, World!')",
		"package-lock.json": "{}",
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &NodeStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "20"})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		data, ok := m["app/index.js"]
		require.True(t, ok)
		require.NotEmpty(t, data)
	})

	t.Run("yarn", func(t *testing.T) {

		// Clean up and test with yarn
		os.RemoveAll(dir)
		root = t.TempDir()
		dir = setupTestDir(root, t)

		delete(files, "package-lock.json")

		files["yarn.lock"] = "{}"
		for name, content := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
		}

		stack = &NodeStack{
			MetaStack: MetaStack{
				dir: dir,
			},
		}

		state, err = stack.GenerateLLB(dir, BuildOptions{Version: "20"})
		require.NoError(t, err)

		buildLLB(t, dir, state, func(r io.Reader) {
			m, err := tarx.TarToMap(r)
			require.NoError(t, err)
			data, ok := m["app/index.js"]
			require.True(t, ok)
			require.NotEmpty(t, data)
		})
	})
}

func TestNodeNextjs(t *testing.T) {
	testCases := []struct {
		name      string
		files     map[string]string
		wantNext  bool
		wantBuild string
		wantWeb   string
	}{
		{
			name: "next in dependencies with npm",
			files: map[string]string{
				"package.json":      `{"name":"app","dependencies":{"next":"14.0.0","react":"^18.0.0"},"scripts":{"build":"next build","start":"next start"}}`,
				"package-lock.json": "{}",
			},
			wantNext:  true,
			wantBuild: "npm run build",
			wantWeb:   "npm run start",
		},
		{
			name: "next with yarn",
			files: map[string]string{
				"package.json": `{"name":"app","dependencies":{"next":"14.0.0"},"scripts":{"build":"next build","start":"next start"}}`,
				"yarn.lock":    "{}",
			},
			wantNext:  true,
			wantBuild: "yarn build",
			wantWeb:   "yarn start",
		},
		{
			name: "next with a custom start script is left alone",
			files: map[string]string{
				"package.json":      `{"name":"app","dependencies":{"next":"14.0.0"},"scripts":{"build":"next build","start":"node server.js"}}`,
				"package-lock.json": "{}",
			},
			wantNext:  true,
			wantBuild: "npm run build",
			wantWeb:   "npm run start",
		},
		{
			name: "next with only a serve script uses it",
			files: map[string]string{
				"package.json":      `{"name":"app","dependencies":{"next":"14.0.0"},"scripts":{"build":"next build","serve":"node serve.js"}}`,
				"package-lock.json": "{}",
			},
			wantNext:  true,
			wantBuild: "npm run build",
			wantWeb:   "npm run serve",
		},
		{
			name: "next with yarn and no start script falls back to next start",
			files: map[string]string{
				"package.json": `{"name":"app","dependencies":{"next":"14.0.0"},"scripts":{"build":"next build"}}`,
				"yarn.lock":    "{}",
			},
			wantNext:  true,
			wantBuild: "yarn build",
			wantWeb:   "yarn next start -p $PORT",
		},
		{
			name: "next listed under devDependencies",
			files: map[string]string{
				"package.json":      `{"name":"app","devDependencies":{"next":"14.0.0"},"scripts":{"build":"next build"}}`,
				"package-lock.json": "{}",
			},
			wantNext:  true,
			wantBuild: "npm run build",
			wantWeb:   "npx next start -p $PORT",
		},
		{
			name: "plain express app is not next",
			files: map[string]string{
				"package.json":      `{"name":"app","dependencies":{"express":"^4.18.2"},"scripts":{"start":"node index.js"}}`,
				"package-lock.json": "{}",
			},
			wantNext:  false,
			wantBuild: "",
			wantWeb:   "npm run start",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			stack := &NodeStack{MetaStack: MetaStack{dir: dir}}
			require.True(t, stack.Detect())
			stack.Init(BuildOptions{})

			require.Equal(t, tc.wantNext, stack.hasNext)
			require.Equal(t, tc.wantBuild, stack.frameworkBuildCommand())
			require.Equal(t, tc.wantWeb, stack.WebCommand())

			// The build graph must construct and marshal cleanly, including the
			// Next.js build step (the AddEnv loop + build.Run) for the Next
			// cases. This covers the GenerateLLB wiring without needing Docker.
			state, err := stack.GenerateLLB(dir, BuildOptions{EnvVars: map[string]string{"NEXT_PUBLIC_FOO": "bar"}})
			require.NoError(t, err)
			_, err = state.Marshal(context.Background())
			require.NoError(t, err)
		})
	}
}

func TestBun(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"package.json": `{
			"name": "test-app",
			"version": "1.0.0",
			"dependencies": {
				"express": "^4.18.2"
			}
		}`,
		"bun.lock": "", // Binary file, empty is fine for test
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &BunStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "1"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestBunDetect(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		expected bool
	}{
		{
			name: "bun.lock",
			files: map[string]string{
				"package.json": `{"name": "app"}`,
				"bun.lock":     "",
			},
			expected: true,
		},
		{
			name: "bun.lockb legacy",
			files: map[string]string{
				"package.json": `{"name": "app"}`,
				"bun.lockb":    "",
			},
			expected: true,
		},
		{
			name: "bunfig.toml",
			files: map[string]string{
				"package.json": `{"name": "app"}`,
				"bunfig.toml":  "[install]\noptional = true\n",
			},
			expected: true,
		},
		{
			name: "packageManager field",
			files: map[string]string{
				"package.json": `{"name": "app", "packageManager": "bun@1.1.0"}`,
			},
			expected: true,
		},
		{
			name: "bun in scripts",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"start": "bun run index.ts"}}`,
			},
			expected: true,
		},
		{
			name: "bun as standalone command in scripts",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"dev": "bun --watch index.ts"}}`,
			},
			expected: true,
		},
		{
			name: "Procfile with bun",
			files: map[string]string{
				"package.json": `{"name": "app"}`,
				"Procfile":     "web: bun run start",
			},
			expected: true,
		},
		{
			name: "plain package.json no bun signals",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"start": "node index.js"}}`,
			},
			expected: false,
		},
		{
			name: "no package.json",
			files: map[string]string{
				"index.ts": "console.log('hi')",
			},
			expected: false,
		},
		{
			name: "bunx in scripts",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"test": "bunx vitest"}}`,
			},
			expected: true,
		},
		{
			name: "bun at end of script command",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"start": "npx something && bun"}}`,
			},
			expected: true,
		},
		{
			name: "bundle in scripts is not bun",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"start": "bundle exec rails server"}}`,
			},
			expected: false,
		},
		{
			name: "packageManager field for npm not bun",
			files: map[string]string{
				"package.json": `{"name": "app", "packageManager": "npm@10.0.0"}`,
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			stack := &BunStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}
			require.Equal(t, tc.expected, stack.Detect())
		})
	}
}

func TestGo(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"go.mod":  readFile(t, "go/go.mod"),
		"go.sum":  readFile(t, "go/go.sum"),
		"main.go": readFile(t, "go/main.go"),
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &GoStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "1.23"})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		// Scan the tar directly rather than via tarx.TarToMap: that helper keeps
		// only regular files, and the busybox shell/coreutils land as symlinks.
		names := map[string]bool{}
		var appData []byte
		tr := tar.NewReader(r)
		for {
			th, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			names[th.Name] = true
			if th.Name == "bin/app" && th.Typeflag == tar.TypeReg {
				appData, err = io.ReadAll(tr)
				require.NoError(t, err)
			}
		}

		require.NotEmpty(t, appData, "built binary should be present at bin/app")

		// Pure-Go lands on the distroless static runtime, but it must still carry
		// a busybox shell and coreutils. /bin/sh because the runner launches the
		// app via `/bin/sh -c` (a shell-less image crash-loops at boot), and the
		// likes of /bin/echo so `miren sandbox exec` can run commands in it. The
		// heavyweight Go toolchain is left behind on the builder.
		require.True(t, names["bin/sh"], "runtime needs /bin/sh; the runner execs the app via /bin/sh -c")
		require.True(t, names["bin/echo"], "runtime needs busybox coreutils for `miren sandbox exec`")
		require.False(t, names["usr/local/go/bin/go"], "runtime image must not carry the Go toolchain")
		require.True(t, names["etc/passwd"], "distroless runtime should have an app-user passwd entry")
	})
}

// TestGoRuntimeIncludesNonGoFiles verifies that a pure-Go app lands on the
// distroless static runtime carrying its non-Go files (README, nested data
// dirs) so it can read them at runtime, while the Go source and module/vendor
// build inputs are left behind on the builder.
func TestGoRuntimeIncludesNonGoFiles(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"go.mod":        readFile(t, "go/go.mod"),
		"go.sum":        readFile(t, "go/go.sum"),
		"main.go":       readFile(t, "go/main.go"),
		"README.md":     "# hello\n",
		"data/seed.txt": "seed\n",
	}

	for name, content := range files {
		full := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}

	stack := &GoStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "1.23"})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		names := map[string]bool{}
		tr := tar.NewReader(r)
		for {
			th, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			names[th.Name] = true
		}

		// Non-Go files travel with the binary onto the distroless runtime.
		require.True(t, names["bin/app"], "built binary should be present at bin/app")
		require.True(t, names["app/README.md"], "non-Go README.md should be carried into the runtime")
		require.True(t, names["app/data/seed.txt"], "nested non-Go data files should be carried into the runtime")

		// Go source and module/vendor build inputs are stripped.
		require.False(t, names["app/main.go"], "Go source must not be shipped in the runtime")
		require.False(t, names["app/go.mod"], "go.mod must not be shipped in the runtime")
		require.False(t, names["app/go.sum"], "go.sum must not be shipped in the runtime")
	})
}

func TestGoCgo(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"go.mod":  readFile(t, "go-cgo/go.mod"),
		"main.go": readFile(t, "go-cgo/main.go"),
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	// cgo is opt-in via the standard CGO_ENABLED build env var.
	opts := BuildOptions{Version: "1.23", EnvVars: map[string]string{"CGO_ENABLED": "1"}}

	stack := &GoStack{MetaStack: MetaStack{dir: dir}}
	stack.Init(opts)
	require.True(t, stack.cgoEnabled)

	state, err := stack.GenerateLLB(dir, opts)
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		// debian-slim has a merged /usr (/bin -> /usr/bin), so the binary
		// copied to /bin/app lands at usr/bin/app; /bin/app still resolves to
		// it via the symlink at runtime.
		data, ok := m["usr/bin/app"]
		if !ok {
			data, ok = m["bin/app"]
		}
		require.True(t, ok, "cgo binary should be present (bin/app or usr/bin/app)")
		require.NotEmpty(t, data)

		// cgo lands on debian-slim (etc/debian_version is present there but not
		// on the distroless static image), with the Go toolchain left behind on
		// the builder.
		_, hasDebian := m["etc/debian_version"]
		require.True(t, hasDebian, "cgo image should ship on debian-slim")
		_, hasToolchain := m["usr/local/go/bin/go"]
		require.False(t, hasToolchain, "runtime image must not carry the Go toolchain")
	})
}

func TestGoWithVendor(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	// Create a simple Go project without external dependencies for vendor test
	files := map[string]string{
		"go.mod": "module test-app\n\ngo 1.23\n",
		"go.sum": "",
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	// Create vendor directory with empty modules.txt (simulating vendored stdlib only)
	vendorDir := filepath.Join(dir, "vendor")
	require.NoError(t, os.MkdirAll(vendorDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(vendorDir, "modules.txt"),
		[]byte(""),
		0644,
	))

	stack := &GoStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "1.23"})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		data, ok := m["bin/app"]
		require.True(t, ok)
		require.NotEmpty(t, data)
	})
}

func TestGoVersionDetection(t *testing.T) {
	// Test the parseGoModVersion function
	testCases := []struct {
		name            string
		goModContent    string
		expectedVersion string
	}{
		{
			name:            "simple version",
			goModContent:    "module test-app\n\ngo 1.23\n",
			expectedVersion: "1.23",
		},
		{
			name:            "patch version",
			goModContent:    "module test-app\n\ngo 1.23.4\n",
			expectedVersion: "1.23.4",
		},
		{
			name:            "with dependencies",
			goModContent:    "module test-app\n\ngo 1.22.1\n\nrequire github.com/gorilla/mux v1.8.1\n",
			expectedVersion: "1.22.1",
		},
		{
			name:            "no go directive",
			goModContent:    "module test-app\n",
			expectedVersion: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.goModContent), 0644))

			stack := &GoStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}

			version := stack.parseGoModVersion()
			require.Equal(t, tc.expectedVersion, version)
		})
	}
}

func TestGoCgoEnvVar(t *testing.T) {
	cgoEnv := func(v string) BuildOptions {
		return BuildOptions{EnvVars: map[string]string{"CGO_ENABLED": v}}
	}

	cases := []struct {
		name string
		opts BuildOptions
		want bool
	}{
		{"defaults to off (static, distroless)", BuildOptions{}, false},
		{"CGO_ENABLED=1 enables cgo", cgoEnv("1"), true},
		{"CGO_ENABLED=0 stays off", cgoEnv("0"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module test-app\n\ngo 1.23\n"), 0644))

			stack := &GoStack{MetaStack: MetaStack{dir: dir}}
			stack.Init(tc.opts)
			require.Equal(t, tc.want, stack.cgoEnabled)
		})
	}
}

func TestRubyVersionDetection(t *testing.T) {
	// Test parseRubyVersion across its sources: .ruby-version takes precedence
	// over the Gemfile's inline ruby directive.
	testCases := []struct {
		name            string
		rubyVersion     string // contents of .ruby-version, "" to skip the file
		gemfileContent  string
		expectedVersion string
	}{
		{
			name:            "ruby-version file",
			rubyVersion:     "3.3.0\n",
			gemfileContent:  "source 'https://rubygems.org'\n",
			expectedVersion: "3.3.0",
		},
		{
			name:            "ruby-version file with ruby- prefix",
			rubyVersion:     "ruby-3.4.1\n",
			gemfileContent:  "source 'https://rubygems.org'\n",
			expectedVersion: "3.4.1",
		},
		{
			name:            "gemfile inline directive",
			gemfileContent:  "source 'https://rubygems.org'\nruby \"3.3\"\n",
			expectedVersion: "3.3",
		},
		{
			name:            "gemfile file directive falls back to ruby-version",
			rubyVersion:     "3.4.2\n",
			gemfileContent:  "source 'https://rubygems.org'\nruby file: \".ruby-version\"\n",
			expectedVersion: "3.4.2",
		},
		{
			name:            "ruby-version wins over gemfile directive",
			rubyVersion:     "3.4.0\n",
			gemfileContent:  "source 'https://rubygems.org'\nruby \"3.3\"\n",
			expectedVersion: "3.4.0",
		},
		{
			name:            "blank ruby-version falls back to gemfile",
			rubyVersion:     "   \n",
			gemfileContent:  "source 'https://rubygems.org'\nruby \"3.3\"\n",
			expectedVersion: "3.3",
		},
		{
			name:            "no version source",
			gemfileContent:  "source 'https://rubygems.org'\n",
			expectedVersion: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(tc.gemfileContent), 0644))
			if tc.rubyVersion != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".ruby-version"), []byte(tc.rubyVersion), 0644))
			}

			stack := &RubyStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}

			version := stack.parseRubyVersion()
			require.Equal(t, tc.expectedVersion, version)
		})
	}
}

func TestRust(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"Cargo.toml":  readFile(t, "rust/Cargo.toml"),
		"Cargo.lock":  readFile(t, "rust/Cargo.lock"),
		"src/main.rs": readFile(t, "rust/main.rs"),
	}

	// Create src directory
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0755))

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &RustStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	require.True(t, stack.Detect())
	stack.Init(BuildOptions{Version: "1"})
	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "1"})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		data, ok := m["bin/app"]
		require.True(t, ok)
		require.NotEmpty(t, data)
	})
}

func TestPythonUv(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"pyproject.toml": readFile(t, "python-uv/pyproject.toml"),
		"uv.lock":        readFile(t, "python-uv/uv.lock"),
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	stack := &PythonStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}

	// Verify uv is detected
	require.True(t, stack.Detect())

	state, err := stack.GenerateLLB(dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestRubyWebCommand(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name: "rails app",
			files: map[string]string{
				"Gemfile":          "gem 'rails'\n",
				"Gemfile.lock":     "rails (7.0.0)\n",
				"config/routes.rb": "",
			},
			expected: "rails server -b 0.0.0.0 -p $PORT",
		},
		{
			name: "puma with config",
			files: map[string]string{
				"Gemfile":        "gem 'puma'\n",
				"Gemfile.lock":   "puma (6.0.0)\n",
				"config/puma.rb": "# puma config",
			},
			expected: "puma -C config/puma.rb",
		},
		{
			name: "puma without config",
			files: map[string]string{
				"Gemfile":      "gem 'puma'\n",
				"Gemfile.lock": "puma (6.0.0)\n",
			},
			expected: "puma -b tcp://0.0.0.0 -p $PORT",
		},
		{
			name: "unicorn",
			files: map[string]string{
				"Gemfile":      "gem 'unicorn'\n",
				"Gemfile.lock": "unicorn (6.0.0)\n",
			},
			expected: "unicorn -p $PORT",
		},
		{
			name: "rack app with config.ru",
			files: map[string]string{
				"Gemfile":      "gem 'sinatra'\n",
				"Gemfile.lock": "sinatra (3.0.0)\n",
				"config.ru":    "run Sinatra::Application",
			},
			expected: "rackup -p $PORT",
		},
		{
			name: "no web server",
			files: map[string]string{
				"Gemfile":      "gem 'nokogiri'\n",
				"Gemfile.lock": "nokogiri (1.0.0)\n",
			},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			// Create config directory if needed
			for name := range tc.files {
				if filepath.Dir(name) != "." {
					require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0755))
				}
			}

			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			stack := &RubyStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}
			require.True(t, stack.Detect())
			stack.Init(BuildOptions{})

			require.Equal(t, tc.expected, stack.WebCommand())
		})
	}
}

func TestPythonWebCommand(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name: "fastapi with main.py",
			files: map[string]string{
				"requirements.txt": "fastapi\nuvicorn\n",
				"main.py":          "from fastapi import FastAPI\napp = FastAPI()",
			},
			expected: "fastapi run main.py --host 0.0.0.0 --port $PORT",
		},
		{
			name: "fastapi with app.py",
			files: map[string]string{
				"requirements.txt": "fastapi\nuvicorn\n",
				"app.py":           "from fastapi import FastAPI\napp = FastAPI()",
			},
			expected: "fastapi run app.py --host 0.0.0.0 --port $PORT",
		},
		{
			name: "gunicorn with wsgi module",
			files: map[string]string{
				"requirements.txt":  "gunicorn\ndjango\n",
				"manage.py":         "",
				"myapp/wsgi.py":     "application = get_wsgi_application()",
				"myapp/__init__.py": "",
			},
			expected: "gunicorn myapp.wsgi:application -b 0.0.0.0:$PORT",
		},
		{
			name: "uvicorn with main.py",
			files: map[string]string{
				"requirements.txt": "uvicorn\nstarlette\n",
				"main.py":          "from starlette.applications import Starlette\napp = Starlette()",
			},
			expected: "uvicorn main:app --host 0.0.0.0 --port $PORT",
		},
		{
			name: "flask",
			files: map[string]string{
				"requirements.txt": "flask\n",
				"app.py":           "from flask import Flask\napp = Flask(__name__)",
			},
			expected: "flask run --host=0.0.0.0 --port=$PORT",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			// Create subdirectories if needed
			for name := range tc.files {
				if filepath.Dir(name) != "." {
					require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0755))
				}
			}

			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			stack := &PythonStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}
			require.True(t, stack.Detect())
			stack.Init(BuildOptions{})

			require.Equal(t, tc.expected, stack.WebCommand())
		})
	}
}

func TestNodeWebCommand(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name: "npm with start script",
			files: map[string]string{
				"package.json":      `{"name": "app", "scripts": {"start": "node index.js"}}`,
				"package-lock.json": "{}",
			},
			expected: "npm run start",
		},
		{
			name: "yarn with start script",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"start": "node index.js"}}`,
				"yarn.lock":    "",
			},
			expected: "yarn start",
		},
		{
			name: "npm with serve script",
			files: map[string]string{
				"package.json":      `{"name": "app", "scripts": {"serve": "node server.js"}}`,
				"package-lock.json": "{}",
			},
			expected: "npm run serve",
		},
		{
			name: "npm with server script",
			files: map[string]string{
				"package.json":      `{"name": "app", "scripts": {"server": "node server.js"}}`,
				"package-lock.json": "{}",
			},
			expected: "npm run server",
		},
		{
			name: "npm with main entry point",
			files: map[string]string{
				"package.json":      `{"name": "app", "main": "index.js"}`,
				"package-lock.json": "{}",
				"index.js":          "",
			},
			expected: "node index.js",
		},
		{
			name: "npm with typescript entry point",
			files: map[string]string{
				"package.json":      `{"name": "app", "main": "index.ts"}`,
				"package-lock.json": "{}",
				"index.ts":          "",
			},
			expected: "npx tsx index.ts",
		},
		{
			name: "no scripts or entry point",
			files: map[string]string{
				"package.json":      `{"name": "app"}`,
				"package-lock.json": "{}",
			},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			stack := &NodeStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}
			require.True(t, stack.Detect())
			stack.Init(BuildOptions{})

			require.Equal(t, tc.expected, stack.WebCommand())
		})
	}
}

func TestBunWebCommand(t *testing.T) {
	testCases := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name: "bun with start script",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"start": "bun index.ts"}}`,
				"bun.lock":     "",
			},
			expected: "bun run start",
		},
		{
			name: "bun with serve script",
			files: map[string]string{
				"package.json": `{"name": "app", "scripts": {"serve": "bun server.ts"}}`,
				"bun.lock":     "",
			},
			expected: "bun run serve",
		},
		{
			name: "bun with main entry point",
			files: map[string]string{
				"package.json": `{"name": "app", "main": "index.ts"}`,
				"bun.lock":     "",
				"index.ts":     "",
			},
			expected: "bun index.ts",
		},
		{
			name: "bun no scripts or entry point",
			files: map[string]string{
				"package.json": `{"name": "app"}`,
				"bun.lock":     "",
			},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
			}

			stack := &BunStack{
				MetaStack: MetaStack{
					dir: dir,
				},
			}
			require.True(t, stack.Detect())
			stack.Init(BuildOptions{})

			require.Equal(t, tc.expected, stack.WebCommand())
		})
	}
}

func TestGoWebCommand(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test-app\n\ngo 1.23\n"), 0644))

	stack := &GoStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	require.True(t, stack.Detect())
	stack.Init(BuildOptions{})

	require.Equal(t, "/bin/app", stack.WebCommand())
}

func TestRustWebCommand(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test-app\"\nversion = \"0.1.0\"\n"), 0644))

	stack := &RustStack{
		MetaStack: MetaStack{
			dir: dir,
		},
	}
	require.True(t, stack.Detect())
	stack.Init(BuildOptions{})

	require.Equal(t, "/bin/app", stack.WebCommand())
}

func TestRustBuildCommand(t *testing.T) {
	t.Run("with package name force-rebuilds workspace crate", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"my-app\"\nversion = \"0.1.0\"\n"), 0644))

		stack := &RustStack{MetaStack: MetaStack{dir: dir}}
		require.True(t, stack.Detect())
		stack.Init(BuildOptions{})

		require.Equal(t, "cargo clean --release -p my-app && cargo build --release", stack.buildCommand())
	})

	t.Run("virtual workspace falls back to bare cargo build", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[workspace]\nmembers = [\"member-a\"]\n"), 0644))

		stack := &RustStack{MetaStack: MetaStack{dir: dir}}
		require.True(t, stack.Detect())
		stack.Init(BuildOptions{})

		require.Empty(t, stack.packageName)
		require.Equal(t, "cargo build --release", stack.buildCommand())
	})
}
