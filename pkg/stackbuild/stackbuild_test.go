package stackbuild

import (
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
		m, err := tarx.TarToMap(r)
		require.NoError(t, err)
		data, ok := m["bin/app"]
		require.True(t, ok)
		require.NotEmpty(t, data)
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
