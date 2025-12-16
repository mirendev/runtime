package build

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	buildkit "github.com/moby/buildkit/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tonistiigi/fsutil"

	"miren.dev/runtime/pkg/imagerefs"

	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
)

func checkDocker() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

// testInfra holds the docker client, network, and cleanup functions for test infrastructure
type testInfra struct {
	docker      *client.Client
	networkID   string
	networkName string
	cleanups    []func()
}

func setupTestInfra(t *testing.T, ctx context.Context) *testInfra {
	t.Helper()

	cl, err := client.NewClientWithOpts(client.FromEnv)
	require.NoError(t, err)

	// Create a unique network for this test
	networkName := "buildkit-test-" + t.Name()
	networkResp, err := cl.NetworkCreate(ctx, networkName, dockernetwork.CreateOptions{})
	require.NoError(t, err)

	infra := &testInfra{
		docker:      cl,
		networkID:   networkResp.ID,
		networkName: networkName,
	}

	// Add network cleanup
	infra.cleanups = append(infra.cleanups, func() {
		if err := cl.NetworkRemove(ctx, networkResp.ID); err != nil {
			t.Logf("failed to remove network: %v", err)
		}
	})

	return infra
}

func (infra *testInfra) cleanup() {
	// Run cleanups in reverse order
	for i := len(infra.cleanups) - 1; i >= 0; i-- {
		infra.cleanups[i]()
	}
}

// setupBuildkitContainer creates a buildkit container and returns a client to it.
// registryHost is the hostname of the insecure registry to allow (e.g., "registry:5000").
func (infra *testInfra) setupBuildkitContainer(t *testing.T, ctx context.Context, registryHost string) *buildkit.Client {
	t.Helper()

	cl := infra.docker

	// Pull buildkit image
	pullReader, err := cl.ImagePull(ctx, imagerefs.BuildKit, image.PullOptions{})
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, pullReader)
	require.NoError(t, err)
	pullReader.Close()

	// Create buildkit config file that allows insecure registry
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "buildkitd.toml")
	configContent := `
[registry."` + registryHost + `"]
  http = true
  insecure = true
`
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Create buildkit container connected to our test network
	resp, err := cl.ContainerCreate(ctx,
		&container.Config{
			Image: imagerefs.BuildKit,
			Cmd:   []string{"--config", "/etc/buildkit/buildkitd.toml"},
		},
		&container.HostConfig{
			Privileged:  true,
			NetworkMode: container.NetworkMode(infra.networkName),
			Binds: []string{
				configPath + ":/etc/buildkit/buildkitd.toml:ro",
			},
		},
		&dockernetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dockernetwork.EndpointSettings{
				infra.networkName: {},
			},
		},
		nil,
		"",
	)
	require.NoError(t, err)

	infra.cleanups = append(infra.cleanups, func() {
		err := cl.ContainerKill(ctx, resp.ID, "KILL")
		if err != nil {
			t.Logf("failed to kill buildkit container: %v", err)
		}
		err = cl.ContainerRemove(ctx, resp.ID, container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		})
		if err != nil {
			t.Logf("failed to remove buildkit container: %v", err)
		}
	})

	// Capture logs for debugging
	var buf bytes.Buffer
	go func() {
		r, err := cl.ContainerLogs(ctx, resp.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if err != nil {
			t.Logf("failed to get buildkit container logs: %v", err)
			return
		}
		defer r.Close()
		io.Copy(&buf, r)
	}()

	err = cl.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	bkc, err := buildkit.New(ctx, "docker-container://"+resp.ID)
	require.NoError(t, err)

	infra.cleanups = append(infra.cleanups, func() {
		bkc.Close()
	})

	// Verify buildkit is ready
	_, err = bkc.Info(ctx)
	require.NoError(t, err)

	return bkc
}

// registryAddrs holds both the network-accessible and host-accessible registry addresses
type registryAddrs struct {
	network string // Address for buildkit to push to (docker network name)
	host    string // Address for host to fetch from (localhost:port)
}

// setupLocalRegistry creates a local registry container and returns both network and host addresses.
func (infra *testInfra) setupLocalRegistry(t *testing.T, ctx context.Context) registryAddrs {
	t.Helper()

	cl := infra.docker

	// Pull registry image
	pullReader, err := cl.ImagePull(ctx, "registry:2", image.PullOptions{})
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, pullReader)
	require.NoError(t, err)
	pullReader.Close()

	registryName := "registry-" + t.Name()

	// Create registry container connected to our test network
	resp, err := cl.ContainerCreate(ctx,
		&container.Config{
			Image: "registry:2",
			ExposedPorts: nat.PortSet{
				"5000/tcp": struct{}{},
			},
		},
		&container.HostConfig{
			PublishAllPorts: true,
			NetworkMode:     container.NetworkMode(infra.networkName),
		},
		&dockernetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dockernetwork.EndpointSettings{
				infra.networkName: {
					Aliases: []string{registryName},
				},
			},
		},
		nil,
		registryName,
	)
	require.NoError(t, err)

	infra.cleanups = append(infra.cleanups, func() {
		err := cl.ContainerKill(ctx, resp.ID, "KILL")
		if err != nil {
			t.Logf("failed to kill registry container: %v", err)
		}
		err = cl.ContainerRemove(ctx, resp.ID, container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		})
		if err != nil {
			t.Logf("failed to remove registry container: %v", err)
		}
	})

	err = cl.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	// Get the mapped port for host access
	inspect, err := cl.ContainerInspect(ctx, resp.ID)
	require.NoError(t, err)
	portBindings := inspect.NetworkSettings.Ports["5000/tcp"]
	require.NotEmpty(t, portBindings, "registry port should be mapped")
	hostPort := portBindings[0].HostPort

	return registryAddrs{
		network: registryName + ":5000",
		host:    "localhost:" + hostPort,
	}
}

func TestBuildImageWorkingDir(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	// Setup test infrastructure
	infra := setupTestInfra(t, ctx)
	defer infra.cleanup()

	// Setup local registry
	registry := infra.setupLocalRegistry(t, ctx)

	// Setup buildkit with the registry as insecure
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
WORKDIR /custom/workdir
RUN echo "test" > /custom/workdir/test.txt
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create fsutil.FS from test directory
	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	// Create Buildkit instance with registry override for config fetching
	bk := &Buildkit{
		Client:              bkc,
		Log:                 slog.Default(),
		RegistryURLOverride: registry.host, // Use localhost:port for fetching config
	}

	// Build the image
	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-workdir:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	// Verify the working directory was extracted
	require.NotEmpty(t, res.ManifestDigest, "ManifestDigest should be set")
	assert.Equal(t, "/custom/workdir", res.WorkingDir, "WorkingDir should match the WORKDIR in Dockerfile")
}

func TestBuildImageWorkingDirRoot(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	// Setup test infrastructure
	infra := setupTestInfra(t, ctx)
	defer infra.cleanup()

	// Setup local registry
	registry := infra.setupLocalRegistry(t, ctx)

	// Setup buildkit with the registry as insecure
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren that has WORKDIR /
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
WORKDIR /
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	bk := &Buildkit{
		Client:              bkc,
		Log:                 slog.Default(),
		RegistryURLOverride: registry.host,
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-workdir-root:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	assert.Equal(t, "/", res.WorkingDir, "WorkingDir should be / when WORKDIR / is set")
}

func TestBuildImageNoWorkdir(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	// Setup test infrastructure
	infra := setupTestInfra(t, ctx)
	defer infra.cleanup()

	// Setup local registry
	registry := infra.setupLocalRegistry(t, ctx)

	// Setup buildkit with the registry as insecure
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren that has no WORKDIR
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
RUN echo "no workdir"
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	bk := &Buildkit{
		Client:              bkc,
		Log:                 slog.Default(),
		RegistryURLOverride: registry.host,
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-no-workdir:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	// When no WORKDIR is set, the default depends on the base image
	// alpine:latest has no WORKDIR, so it should be empty or "/"
	assert.True(t, res.WorkingDir == "" || res.WorkingDir == "/",
		"WorkingDir should be empty or / when no WORKDIR is set, got: %q", res.WorkingDir)
}

func TestBuildImageEntrypointAndCmd(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	// Setup test infrastructure
	infra := setupTestInfra(t, ctx)
	defer infra.cleanup()

	// Setup local registry
	registry := infra.setupLocalRegistry(t, ctx)

	// Setup buildkit with the registry as insecure
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren that has ENTRYPOINT and CMD
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
WORKDIR /app
ENTRYPOINT ["node"]
CMD ["server.js"]
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	bk := &Buildkit{
		Client:              bkc,
		Log:                 slog.Default(),
		RegistryURLOverride: registry.host,
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-entrypoint:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	// Verify entrypoint and cmd were extracted
	assert.Equal(t, "node", res.Entrypoint, "Entrypoint should match ENTRYPOINT in Dockerfile")
	assert.Equal(t, "server.js", res.Command, "Command should match CMD in Dockerfile")
	assert.Equal(t, "/app", res.WorkingDir, "WorkingDir should match WORKDIR in Dockerfile")
}

func TestBuildImageCmdOnly(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	// Setup test infrastructure
	infra := setupTestInfra(t, ctx)
	defer infra.cleanup()

	// Setup local registry
	registry := infra.setupLocalRegistry(t, ctx)

	// Setup buildkit with the registry as insecure
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren that has only CMD
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
CMD ["npm", "start"]
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	bk := &Buildkit{
		Client:              bkc,
		Log:                 slog.Default(),
		RegistryURLOverride: registry.host,
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-cmd-only:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	// Verify cmd was extracted (entrypoint should be empty for alpine)
	assert.Equal(t, "npm start", res.Command, "Command should match CMD in Dockerfile")
	assert.Equal(t, "", res.Entrypoint, "Entrypoint should be empty when not set")
}

func TestBuildImageNestedWorkdir(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	// Setup test infrastructure
	infra := setupTestInfra(t, ctx)
	defer infra.cleanup()

	// Setup local registry
	registry := infra.setupLocalRegistry(t, ctx)

	// Setup buildkit with the registry as insecure
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren with deeply nested WORKDIR
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
WORKDIR /var/www/html/app/current
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	bk := &Buildkit{
		Client:              bkc,
		Log:                 slog.Default(),
		RegistryURLOverride: registry.host,
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-nested-workdir:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	assert.Equal(t, "/var/www/html/app/current", res.WorkingDir,
		"WorkingDir should match deeply nested WORKDIR")
}
