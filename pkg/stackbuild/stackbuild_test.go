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
	_, err = cl.ImagePull(ctx, "moby/buildkit:latest", image.PullOptions{})
	require.NoError(t, err)

	// Create buildkit container
	resp, err := cl.ContainerCreate(ctx,
		&container.Config{
			Image: "moby/buildkit:latest",
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
		"bun.lockb": "", // Binary file, empty is fine for test
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
