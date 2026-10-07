package stackbuild

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/tarx"

	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
)

// helper function to execute LLB locally
func buildLLB(t *testing.T, dir string, state *llb.State, check ...func(f io.Reader)) {
	t.Helper()
	// A cache dir on the host carries layers between the throwaway buildkitd
	// containers the Docker path starts, but parallel tests would race on its
	// index, so it's opt-in for serial runs (go test -parallel 1).
	solveLLB(t, startBuildkit(t), os.Getenv("STACKBUILD_TEST_CACHE"), dir, state, check...)
}

// startBuildkit returns a client for a running buildkitd, starting a throwaway
// one in Docker when there isn't one already. Solving more than once against
// the same client shares its cache, which is how a test checks what a rebuild
// reuses.
func startBuildkit(t *testing.T) *buildkit.Client {
	t.Helper()
	ctx := context.Background()

	if addr := localBuildkitAddr(); addr != "" {
		c, err := buildkit.New(ctx, addr)
		require.NoError(t, err)
		t.Cleanup(func() { c.Close() })
		_, err = c.Info(ctx)
		require.NoError(t, err)
		return c
	}

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

	t.Cleanup(func() {
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
	})

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
	t.Cleanup(func() { c.Close() })

	_, err = c.Info(ctx)
	require.NoError(t, err)

	return c
}

// solvedVertex is a step from a solve, with the output it logged.
type solvedVertex struct {
	*buildkit.Vertex
	Log string
}

// solveLLB builds state on c, runs each check against the exported tar, and
// returns the vertices the solve reported, so a caller can see which steps
// came from cache. A non-empty cacheDir imports and exports a cache on the
// host. Leave it empty to rely only on c's own cache, as the cluster builder
// does; the export keeps only final image layers, so builder steps restored
// from it are never stored locally and miss on the next solve.
func solveLLB(t *testing.T, c *buildkit.Client, cacheDir, dir string, state *llb.State, check ...func(f io.Reader)) map[string]*solvedVertex {
	t.Helper()
	return solveLLBWithSecrets(t, c, cacheDir, dir, state, nil, check...)
}

// solveLLBWithSecrets is solveLLB with build secrets on the solve's session,
// keyed by id, as the build server attaches an app's [[build.secrets]].
func solveLLBWithSecrets(t *testing.T, c *buildkit.Client, cacheDir, dir string, state *llb.State, secrets map[string][]byte, check ...func(f io.Reader)) map[string]*solvedVertex {
	t.Helper()
	ctx := context.Background()

	def, err := state.Marshal(ctx)
	require.NoError(t, err)

	pw, err := progresswriter.NewPrinter(ctx, os.Stdout, "plain")
	require.NoError(t, err)

	// Tee the status stream: the printer gets everything, and we keep the
	// final state of each vertex by name, with its log.
	vertices := map[string]*solvedVertex{}
	byDigest := map[digest.Digest]*solvedVertex{}
	status := make(chan *buildkit.SolveStatus)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(pw.Status())
		for st := range status {
			for _, v := range st.Vertexes {
				sv := byDigest[v.Digest]
				if sv == nil {
					sv = &solvedVertex{}
					byDigest[v.Digest] = sv
				}
				sv.Vertex = v
				vertices[v.Name] = sv
			}
			for _, l := range st.Logs {
				if sv := byDigest[l.Vertex]; sv != nil {
					sv.Log += string(l.Data)
				}
			}
			pw.Status() <- st
		}
	}()

	f, err := os.CreateTemp(t.TempDir(), "buildkit-llb")
	require.NoError(t, err)

	defer f.Close()

	cfg, err := config.Load(config.Dir())
	require.NoError(t, err)

	da := authprovider.NewDockerAuthProvider(cfg, nil)

	solveOpt := buildkit.SolveOpt{
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
	}
	if len(secrets) > 0 {
		solveOpt.Session = append(solveOpt.Session, secretsprovider.FromMap(secrets))
	}
	if cacheDir != "" {
		solveOpt.CacheExports = []buildkit.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"dest": cacheDir,
				},
			},
		}
		solveOpt.CacheImports = []buildkit.CacheOptionsEntry{
			{
				Type: "local",
				Attrs: map[string]string{
					"src": cacheDir,
				},
			},
		}
	}

	_, err = c.Solve(ctx, def, solveOpt, status)
	<-done
	<-pw.Done()
	require.NoError(t, err)

	f, err = os.Open(f.Name())
	require.NoError(t, err)

	for _, cf := range check {
		f.Seek(0, io.SeekStart)
		cf(f)
	}

	return vertices
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

// localBuildkitSocket is where the iso test container's buildkitd listens
// (hack/common-setup.sh), which is how these tests run in CI.
const localBuildkitSocket = "/run/buildkit/buildkitd.sock"

// localBuildkitAddr returns the address of an already-running buildkitd, or ""
// when the tests should start their own in Docker.
func localBuildkitAddr() string {
	if addr := os.Getenv("BUILDKIT_HOST"); addr != "" {
		return addr
	}
	if _, err := os.Stat(localBuildkitSocket); err == nil {
		return "unix://" + localBuildkitSocket
	}
	return ""
}

// runNonce returns a manifest comment unique to this test run, so the builds
// that include it don't hit cache entries left by earlier runs.
func runNonce(comment string) string {
	return fmt.Sprintf("\n%s test run %d\n", comment, time.Now().UnixNano())
}

// requireBuildkit skips the test unless it can reach a buildkitd, either one
// already running or one it can start in Docker. STACKBUILD_SKIP_BUILDKIT
// skips regardless: CI's general test runners set it, since these tests pull
// images and packages from upstream and get a job of their own.
func requireBuildkit(t *testing.T) {
	t.Helper()
	if os.Getenv("STACKBUILD_SKIP_BUILDKIT") != "" {
		t.Skip("STACKBUILD_SKIP_BUILDKIT is set")
	}
	if localBuildkitAddr() != "" {
		return
	}
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		t.Skip("no buildkitd or Docker available")
	}
}

func TestRails(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	opts := BuildOptions{Version: "3.2"}
	stack.Init(opts)
	state, err := stack.GenerateLLB(context.Background(), dir, opts)
	require.NoError(t, err)

	buildLLB(t, dir, state)

	// The start command reaches the app through its Procfile, not the image
	// entrypoint, so check the stack's default for it.
	require.Equal(t, "rails server -b 0.0.0.0 -p $PORT", stack.WebCommand())
}

func TestRuby(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	opts := BuildOptions{Version: "3.2"}
	stack.Init(opts)
	state, err := stack.GenerateLLB(context.Background(), dir, opts)
	require.NoError(t, err)

	buildLLB(t, dir, state)

	// The start command reaches the app through its Procfile, not the image
	// entrypoint, so check the stack's default for it.
	require.Equal(t, "puma -b tcp://0.0.0.0 -p $PORT", stack.WebCommand())
}

func TestPython(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "3.11"})
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

	state, err = stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestPythonPoetry(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestNode(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "20"})
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

		state, err = stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "20"})
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
			state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{EnvVars: map[string]string{"NEXT_PUBLIC_FOO": "bar"}})
			require.NoError(t, err)
			_, err = state.Marshal(context.Background())
			require.NoError(t, err)
		})
	}
}

func TestBun(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "1"})
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
	requireBuildkit(t)
	t.Parallel()

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
	opts := BuildOptions{Version: "1.23"}
	stack.Init(opts)
	require.True(t, stack.splitDeps, "a plain module should compile its dependencies in their own layer")

	state, err := stack.GenerateLLB(context.Background(), dir, opts)
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

// TestGoDepsLayerSurvivesSourceEdits verifies that the compiled dependencies
// are keyed on what the app imports rather than on its source: an edit that
// keeps the imports reuses them, and a changed import set reruns the step.
func TestGoDepsLayerSurvivesSourceEdits(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

	root := t.TempDir()
	dir := setupTestDir(root, t)

	mainGo := readFile(t, "go/main.go")
	files := map[string]string{
		// The nonce gives this run its own cache keys, so the first build is cold
		// even on a buildkitd that outlives test runs, as iso's does.
		"go.mod":  readFile(t, "go/go.mod") + runNonce("//"),
		"go.sum":  readFile(t, "go/go.sum"),
		"main.go": mainGo,
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	build := func(c *buildkit.Client) map[string]*solvedVertex {
		stack := &GoStack{MetaStack: MetaStack{dir: dir}}
		opts := BuildOptions{Version: "1.23"}
		stack.Init(opts)
		state, err := stack.GenerateLLB(context.Background(), dir, opts)
		require.NoError(t, err)
		return solveLLB(t, c, "", dir, state)
	}

	const (
		compileDeps = "[phase] Compiling Go dependencies"
		buildApp    = "[phase] Building Go application"
	)

	c := startBuildkit(t)

	first := build(c)
	require.Contains(t, first, compileDeps)
	require.False(t, first[compileDeps].Cached, "first build has nothing to reuse")

	// Same imports, different code.
	edited := strings.Replace(mainGo, "Hello, World!", "Hello again!", 1)
	require.NotEqual(t, mainGo, edited)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(edited), 0644))

	second := build(c)
	require.True(t, second[compileDeps].Cached, "a source edit that keeps the imports should reuse the compiled dependencies")
	require.False(t, second[buildApp].Cached, "the application itself should rebuild")

	// Dropping the only non-local import changes the list, so the dependency
	// step reruns.
	withoutImport := strings.Replace(edited, `_ "github.com/gorilla/mux"`, "", 1)
	require.NotEqual(t, edited, withoutImport)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(withoutImport), 0644))

	third := build(c)
	require.False(t, third[compileDeps].Cached, "a changed import set should rerun the dependency step")
}

// TestGoRuntimeIncludesNonGoFiles verifies that a pure-Go app lands on the
// distroless static runtime carrying its non-Go files (README, nested data
// dirs) so it can read them at runtime, while the Go source and module/vendor
// build inputs are left behind on the builder.
func TestGoRuntimeIncludesNonGoFiles(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "1.23"})
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
	requireBuildkit(t)
	t.Parallel()

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

	state, err := stack.GenerateLLB(context.Background(), dir, opts)
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

// TestGoWithJSAugmentation builds a Go app that ships a package.json for its
// frontend, the case augmentations exist for. The JS install runs as the app
// user, so the builder needs that user and an /app it can write to, even
// though compileDeps creates /app first.
func TestGoWithJSAugmentation(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

	root := t.TempDir()
	dir := setupTestDir(root, t)

	files := map[string]string{
		"go.mod":            readFile(t, "go/go.mod") + runNonce("//"),
		"go.sum":            readFile(t, "go/go.sum"),
		"main.go":           readFile(t, "go/main.go"),
		"package.json":      `{"name":"assets","version":"1.0.0","dependencies":{"is-plain-obj":"4.1.0"}}`,
		"package-lock.json": "{}",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	// DetectStack would pick Node for a package.json with dependencies, so
	// build the Go stack the way it does for one without.
	opts := BuildOptions{Version: "1.23"}
	stack := &GoStack{MetaStack: MetaStack{dir: dir}}
	stack.Init(opts)
	attachAugmentations(stack, dir)
	require.Equal(t, []Augmentation{AugNpm}, stack.Augmentations())
	require.True(t, stack.splitDeps)

	state, err := stack.GenerateLLB(context.Background(), dir, opts)
	require.NoError(t, err)

	solveLLB(t, startBuildkit(t), "", dir, state, func(f io.Reader) {
		found := map[string]bool{}
		tr := tar.NewReader(f)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			found[hdr.Name] = true
		}
		// debian-slim's /bin is a symlink to /usr/bin.
		require.True(t, found["usr/bin/app"], "Go binary missing")
		require.True(t, found["app/node_modules/is-plain-obj/package.json"], "JS dependency missing")
	})
}

func TestGoDepsSplitBlocker(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		blocked string
	}{
		{
			name:  "plain module",
			files: map[string]string{"go.mod": "module example.com/app\n\ngo 1.23\n\nrequire github.com/gorilla/mux v1.8.1\n"},
		},
		{
			name: "replace with another module version",
			files: map[string]string{"go.mod": "module example.com/app\n\ngo 1.23\n\n" +
				"replace github.com/gorilla/mux => github.com/example/mux v1.8.2\n"},
		},
		{
			name: "replace with a local directory",
			files: map[string]string{"go.mod": "module example.com/app\n\ngo 1.23\n\n" +
				"replace example.com/lib => ./lib\n"},
			blocked: "go.mod replaces example.com/lib with a local directory",
		},
		{
			name: "workspace",
			files: map[string]string{
				"go.mod":  "module example.com/app\n\ngo 1.23\n",
				"go.work": "go 1.23\n\nuse .\n",
			},
			blocked: "go.work workspace",
		},
		{
			name: "vendored",
			files: map[string]string{
				"go.mod":             "module example.com/app\n\ngo 1.23\n",
				"vendor/modules.txt": "",
			},
			blocked: "vendor directory",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(dir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0644))
			}

			stack := &GoStack{MetaStack: MetaStack{dir: dir}}
			stack.Init(BuildOptions{})

			require.Equal(t, tc.blocked, stack.depsSplitBlocker())
			require.Equal(t, tc.blocked == "", stack.splitDeps)
		})
	}
}

func TestGoWithVendor(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "1.23"})
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
	requireBuildkit(t)
	t.Parallel()

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
	require.True(t, stack.splitDeps, "a single-package crate should compile its dependencies in their own layer")
	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "1"})
	require.NoError(t, err)

	buildLLB(t, dir, state, func(r io.Reader) {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		// /bin is a symlink to usr/bin in the rust image.
		require.NotEmpty(t, m["usr/bin/app"], "built binary should be present at /bin/app")
		for name := range m {
			require.False(t, strings.HasPrefix(name, "app/target/"),
				"the compiled dependencies must stay on the builder, found %s", name)
		}
	})
}

// TestRustDepsLayerSurvivesSourceEdits verifies that the compiled dependencies
// are keyed on Cargo.toml and Cargo.lock, and that a source edit still reaches
// the binary despite the placeholder build that came before it.
func TestRustDepsLayerSurvivesSourceEdits(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

	root := t.TempDir()
	dir := setupTestDir(root, t)

	mainRs := readFile(t, "rust-deps/src/main.rs")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0755))
	for name, content := range map[string]string{
		// See TestGoDepsLayerSurvivesSourceEdits for the nonce.
		"Cargo.toml":  readFile(t, "rust-deps/Cargo.toml") + runNonce("#"),
		"Cargo.lock":  readFile(t, "rust-deps/Cargo.lock"),
		"src/main.rs": mainRs,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	binary := func(r io.Reader) []byte {
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		return m["usr/bin/app"]
	}

	var bin []byte
	build := func(c *buildkit.Client) map[string]*solvedVertex {
		stack := &RustStack{MetaStack: MetaStack{dir: dir}}
		require.True(t, stack.Detect())
		opts := BuildOptions{Version: "1"}
		stack.Init(opts)
		require.True(t, stack.splitDeps)
		state, err := stack.GenerateLLB(context.Background(), dir, opts)
		require.NoError(t, err)
		return solveLLB(t, c, "", dir, state, func(r io.Reader) { bin = binary(r) })
	}

	const (
		compileDeps = "[phase] Compiling Rust dependencies"
		buildApp    = "[phase] Building Rust application"
	)

	c := startBuildkit(t)

	first := build(c)
	require.Contains(t, first, compileDeps)
	require.False(t, first[compileDeps].Cached, "first build has nothing to reuse")
	require.Contains(t, string(bin), "greeting number", "the first binary should be the app, not the placeholder")

	edited := strings.Replace(mainRs, "greeting number", "salutation number", 1)
	require.NotEqual(t, mainRs, edited)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/main.rs"), []byte(edited), 0644))

	second := build(c)
	require.True(t, second[compileDeps].Cached, "a source edit should reuse the compiled dependencies")
	require.False(t, second[buildApp].Cached, "the application itself should rebuild")
	require.Contains(t, string(bin), "salutation number", "the rebuilt binary should carry the edit")
	// A cached dependency step only helps if the app build can use it. Any
	// drift between the two (flags, features, toolchain) would show up as
	// cargo compiling the dependency again here.
	require.Contains(t, second[buildApp].Log, "Compiling deps-app")
	require.NotContains(t, second[buildApp].Log, "Compiling itoa",
		"the application build should reuse the dependencies compiled in their own layer")
}

func TestRustDepsSplitBlocker(t *testing.T) {
	const lock = "version = 4\n\n[[package]]\nname = \"app\"\nversion = \"0.1.0\"\n"
	const pkg = "[package]\nname = \"app\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"

	cases := []struct {
		name    string
		files   map[string]string
		blocked string
	}{
		{
			name:  "single package",
			files: map[string]string{"Cargo.toml": pkg, "Cargo.lock": lock, "src/main.rs": ""},
		},
		{
			name:  "single package with a library",
			files: map[string]string{"Cargo.toml": pkg, "Cargo.lock": lock, "src/main.rs": "", "src/lib.rs": ""},
		},
		{
			name:    "virtual workspace",
			files:   map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"a\"]\n", "Cargo.lock": lock},
			blocked: "cargo workspace",
		},
		{
			name:    "package that is also a workspace root",
			files:   map[string]string{"Cargo.toml": pkg + "\n[workspace]\n", "Cargo.lock": lock, "src/main.rs": ""},
			blocked: "cargo workspace",
		},
		{
			name:    "build script",
			files:   map[string]string{"Cargo.toml": pkg, "Cargo.lock": lock, "src/main.rs": "", "build.rs": ""},
			blocked: "build script",
		},
		{
			name:    "explicit binary target",
			files:   map[string]string{"Cargo.toml": pkg + "\n[[bin]]\nname = \"x\"\npath = \"bin/x.rs\"\n", "Cargo.lock": lock, "src/main.rs": ""},
			blocked: "custom target layout",
		},
		{
			name:    "declared bench target",
			files:   map[string]string{"Cargo.toml": pkg + "\n[[bench]]\nname = \"x\"\nharness = false\n", "Cargo.lock": lock, "src/main.rs": ""},
			blocked: "custom target layout",
		},
		{
			name: "vendored through cargo config",
			files: map[string]string{
				"Cargo.toml":         pkg,
				"Cargo.lock":         lock,
				"src/main.rs":        "",
				".cargo/config.toml": "[source.crates-io]\nreplace-with = \"vendored-sources\"\n\n[source.vendored-sources]\ndirectory = \"vendor\"\n",
			},
			blocked: "vendored dependencies",
		},
		{
			name: "cargo config naming a private registry",
			files: map[string]string{
				"Cargo.toml":         pkg,
				"Cargo.lock":         lock,
				"src/main.rs":        "",
				".cargo/config.toml": "[registries.private]\nindex = \"sparse+https://cargo.example.com/index/\"\n",
			},
		},
		{
			name:    "binaries under src/bin",
			files:   map[string]string{"Cargo.toml": pkg, "Cargo.lock": lock, "src/bin/x.rs": ""},
			blocked: "custom target layout",
		},
		{
			name:    "no lockfile",
			files:   map[string]string{"Cargo.toml": pkg, "src/main.rs": ""},
			blocked: "no Cargo.lock",
		},
		{
			name: "path dependency",
			files: map[string]string{
				"Cargo.toml":  pkg,
				"Cargo.lock":  lock + "\n[[package]]\nname = \"lib\"\nversion = \"0.1.0\"\n",
				"src/main.rs": "",
			},
			blocked: "path dependencies",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(dir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0644))
			}

			stack := &RustStack{MetaStack: MetaStack{dir: dir}}
			require.True(t, stack.Detect())
			stack.Init(BuildOptions{})

			require.Equal(t, tc.blocked, stack.depsSplitBlocker(stack.parseCargoToml()))
			require.Equal(t, tc.blocked == "", stack.splitDeps)
		})
	}
}

func TestPythonUv(t *testing.T) {
	requireBuildkit(t)
	t.Parallel()

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

	state, err := stack.GenerateLLB(context.Background(), dir, BuildOptions{Version: "3.11"})
	require.NoError(t, err)

	buildLLB(t, dir, state)
}

func TestRubyDetectGem(t *testing.T) {
	cases := []struct {
		name    string
		gemfile string
		lock    string
		want    bool
	}{
		{name: "declared and locked", gemfile: "gem 'rails'\n", lock: "GEM\n  specs:\n    rails (7.1.0)\n", want: true},
		{name: "declared, double quotes, no lockfile", gemfile: "gem \"rails\", \"~> 7.1\"\n", want: true},
		{name: "declared with parentheses", gemfile: "gem(\"rails\")\n", want: true},
		{name: "only transitive in the lockfile", gemfile: "gem 'mylib'\n", lock: "GEM\n  specs:\n    mylib (1.0)\n      rails (>= 7)\n    rails (7.1.0)\n", want: true},
		{name: "commented out", gemfile: "# gem \"rails\"\ngem \"sinatra\"\n", lock: "GEM\n  specs:\n    sinatra (4.1.0)\n"},
		{name: "longer gem names", gemfile: "gem 'sprockets-rails'\n", lock: "GEM\n  specs:\n    rails-html-sanitizer (1.6.0)\n    sprockets-rails (3.4.2)\n"},
		{name: "mentioned in a string", gemfile: "gem 'sinatra' # not rails\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(tc.gemfile), 0644))
			if tc.lock != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "Gemfile.lock"), []byte(tc.lock), 0644))
			}

			stack := &RubyStack{MetaStack: MetaStack{dir: dir}}
			require.Equal(t, tc.want, stack.detectGem("rails"))
		})
	}
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
