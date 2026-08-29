package build

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tonistiigi/fsutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/workloadidentity"

	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
)

const (
	// testResourcePrefix is used to identify test resources for cleanup
	testResourcePrefix = "buildkit-test-"
	// cleanupTimeout is the maximum time to wait for cleanup operations
	cleanupTimeout = 30 * time.Second
)

func checkDocker() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

func TestAddRegistryAuthProvidesBuildKitToken(t *testing.T) {
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example.com",
	})
	require.NoError(t, err)

	bk := &Buildkit{WorkloadIssuer: issuer}
	var solveOpt buildkit.SolveOpt
	require.NoError(t, bk.addRegistryAuth(&solveOpt, "cluster.local:5000"))
	require.Len(t, solveOpt.Session, 1)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	solveOpt.Session[0].Register(server)
	go server.Serve(listener)
	t.Cleanup(func() {
		server.Stop()
		listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	response, err := auth.NewAuthClient(conn).FetchToken(context.Background(), &auth.FetchTokenRequest{
		Host: "cluster.local:5000",
	})
	require.NoError(t, err)

	_, err = issuer.VerifySystemWorkloadToken(
		response.Token,
		ocireg.Audience,
		workloadidentity.SystemWorkloadBuildKit,
	)
	require.NoError(t, err)
}

func TestAddRegistryAuthWithoutIssuer(t *testing.T) {
	bk := &Buildkit{}
	var solveOpt buildkit.SolveOpt

	require.NoError(t, bk.addRegistryAuth(&solveOpt, "cluster.local:5000"))
	assert.Empty(t, solveOpt.Session)
}

func TestAddRegistryAuthIgnoresExternalRegistry(t *testing.T) {
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example.com",
	})
	require.NoError(t, err)

	bk := &Buildkit{WorkloadIssuer: issuer}
	var solveOpt buildkit.SolveOpt

	require.NoError(t, bk.addRegistryAuth(&solveOpt, "registry.example.com"))
	assert.Empty(t, solveOpt.Session)
}

type testInfra struct {
	t           *testing.T
	docker      *client.Client
	networkID   string
	networkName string
}

func setupTestInfra(t *testing.T) *testInfra {
	t.Helper()

	cl, err := client.NewClientWithOpts(client.FromEnv)
	require.NoError(t, err)

	networkName := testResourcePrefix + sanitizeTestName(t.Name())
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	networkResp, err := cl.NetworkCreate(ctx, networkName, dockernetwork.CreateOptions{})
	require.NoError(t, err)

	infra := &testInfra{
		t:           t,
		docker:      cl,
		networkID:   networkResp.ID,
		networkName: networkName,
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cleanupCancel()

		if err := cl.NetworkRemove(cleanupCtx, networkResp.ID); err != nil {
			t.Errorf("failed to remove network %s: %v", networkName, err)
		}
	})

	return infra
}

func sanitizeTestName(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func (infra *testInfra) setupBuildkitContainer(t *testing.T, ctx context.Context, registryHost string) *buildkit.Client {
	t.Helper()

	cl := infra.docker

	pullReader, err := cl.ImagePull(ctx, imagerefs.BuildKit, image.PullOptions{})
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, pullReader)
	require.NoError(t, err)
	pullReader.Close()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "buildkitd.toml")
	configContent := `
[registry."` + registryHost + `"]
  http = true
  insecure = true
`
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	containerName := testResourcePrefix + "buildkit-" + sanitizeTestName(t.Name())

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
		containerName,
	)
	require.NoError(t, err)

	containerID := resp.ID

	err = cl.ContainerStart(ctx, containerID, container.StartOptions{})
	require.NoError(t, err)

	bkc, err := buildkit.New(ctx, "docker-container://"+containerID)
	require.NoError(t, err)

	t.Cleanup(func() {
		bkc.Close()

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cleanupCancel()

		_ = cl.ContainerKill(cleanupCtx, containerID, "KILL")
		if err := cl.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		}); err != nil {
			t.Errorf("failed to remove buildkit container %s: %v", containerName, err)
		}
	})

	// Verify buildkit is ready
	_, err = bkc.Info(ctx)
	require.NoError(t, err)

	return bkc
}

type registryAddrs struct {
	network string
	host    string
}

func (infra *testInfra) setupLocalRegistry(t *testing.T, ctx context.Context) registryAddrs {
	t.Helper()

	cl := infra.docker

	pullReader, err := cl.ImagePull(ctx, "registry:2", image.PullOptions{})
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, pullReader)
	require.NoError(t, err)
	pullReader.Close()

	registryName := testResourcePrefix + "registry-" + sanitizeTestName(t.Name())

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

	containerID := resp.ID

	err = cl.ContainerStart(ctx, containerID, container.StartOptions{})
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cleanupCancel()

		_ = cl.ContainerKill(cleanupCtx, containerID, "KILL")
		if err := cl.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		}); err != nil {
			t.Errorf("failed to remove registry container %s: %v", registryName, err)
		}
	})

	inspect, err := cl.ContainerInspect(ctx, containerID)
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

	infra := setupTestInfra(t)

	registry := infra.setupLocalRegistry(t, ctx)
	bkc := infra.setupBuildkitContainer(t, ctx, registry.network)

	// Create test directory with Dockerfile.miren
	testDir := t.TempDir()
	dockerfile := `FROM alpine:latest
WORKDIR /custom/workdir
EXPOSE 4000
RUN echo "test" > /custom/workdir/test.txt
`
	err := os.WriteFile(filepath.Join(testDir, "Dockerfile.miren"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create fsutil.FS from test directory
	dfs, err := fsutil.NewFS(testDir)
	require.NoError(t, err)

	bk := &Buildkit{
		Client: bkc,
		Log:    slog.Default(),
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
	assert.Equal(t, []string{"4000/tcp"}, res.ExposedPorts, "ExposedPorts should match EXPOSE in Dockerfile")
}

func TestBuildImageWorkingDirRoot(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	infra := setupTestInfra(t)

	registry := infra.setupLocalRegistry(t, ctx)
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
		Client: bkc,
		Log:    slog.Default(),
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

	infra := setupTestInfra(t)

	registry := infra.setupLocalRegistry(t, ctx)
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
		Client: bkc,
		Log:    slog.Default(),
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

	infra := setupTestInfra(t)

	registry := infra.setupLocalRegistry(t, ctx)
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
		Client: bkc,
		Log:    slog.Default(),
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-entrypoint:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	// The image's ENTRYPOINT/CMD are intentionally not flattened into
	// BuildResult; they run in exec form at launch via oci.WithImageConfig
	// (MIR-1444). BuildImage only extracts WorkingDir here.
	assert.Equal(t, "/app", res.WorkingDir, "WorkingDir should match WORKDIR in Dockerfile")
	assert.Equal(t, "", res.Entrypoint, "Entrypoint runs in exec form at launch, not surfaced in BuildResult")
	assert.Equal(t, "", res.Command, "Command runs in exec form at launch, not surfaced in BuildResult")
}

func TestBuildImageCmdOnly(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	infra := setupTestInfra(t)

	registry := infra.setupLocalRegistry(t, ctx)
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
		Client: bkc,
		Log:    slog.Default(),
	}

	bs := BuildStack{
		Stack:   "dockerfile",
		CodeDir: testDir,
		Input:   "Dockerfile.miren",
	}

	imageURL := registry.network + "/test-cmd-only:latest"

	res, err := bk.BuildImage(ctx, dfs, bs, "test-app", imageURL)
	require.NoError(t, err)

	// CMD runs in exec form at launch (MIR-1444), so BuildImage does not
	// surface it as a flattened command string in BuildResult.
	assert.Equal(t, "", res.Command, "Command runs in exec form at launch, not surfaced in BuildResult")
	assert.Equal(t, "", res.Entrypoint, "Entrypoint should be empty when not set")
}

func TestBuildImageNestedWorkdir(t *testing.T) {
	if !checkDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	infra := setupTestInfra(t)

	registry := infra.setupLocalRegistry(t, ctx)
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
		Client: bkc,
		Log:    slog.Default(),
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
