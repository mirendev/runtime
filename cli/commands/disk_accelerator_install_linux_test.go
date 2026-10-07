package commands

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/lbdmod/ctrbuild"
	"miren.dev/runtime/pkg/runnerconfig"
)

func TestCurrentDiskAcceleratorNode(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "runner.yaml")
	unitPath := filepath.Join(dir, "miren.service")
	serverPath := filepath.Join(dir, "server.toml")
	ctx := &Context{}

	_, _, _, err := currentDiskAcceleratorNode(ctx, runnerPath, unitPath, serverPath)
	require.ErrorContains(t, err, "pass a node name or ID")

	require.NoError(t, os.WriteFile(unitPath, nil, 0644))
	require.NoError(t, os.WriteFile(serverPath, []byte("[server]\nrunner_id = 'coordinator-99'\nconfig_cluster_name = 'this-host'\n"), 0644))
	id, runner, cluster, err := currentDiskAcceleratorNode(ctx, runnerPath, unitPath, serverPath)
	require.NoError(t, err)
	require.Equal(t, "coordinator-99", id)
	require.Nil(t, runner)
	require.Equal(t, "this-host", cluster)
	require.NoError(t, os.WriteFile(serverPath, []byte("[server]\n"), 0644))
	id, _, cluster, err = currentDiskAcceleratorNode(ctx, runnerPath, unitPath, serverPath)
	require.NoError(t, err)
	require.Equal(t, "miren", id)
	require.Equal(t, "local", cluster)
	t.Setenv("MIREN_SERVER_RUNNER_ID", "remote-env-node")
	t.Setenv("MIREN_SERVER_CONFIG_CLUSTER_NAME", "remote-env-cluster")
	id, _, cluster, err = currentDiskAcceleratorNode(ctx, runnerPath, unitPath, serverPath)
	require.NoError(t, err)
	require.Equal(t, "miren", id)
	require.Equal(t, "local", cluster)

	require.NoError(t, (&runnerconfig.Config{RunnerID: "runner-42"}).Save(runnerPath))
	id, runner, cluster, err = currentDiskAcceleratorNode(ctx, runnerPath, unitPath, serverPath)
	require.NoError(t, err)
	require.Equal(t, "runner-42", id)
	require.Equal(t, "runner-42", runner.RunnerID)
	require.Empty(t, cluster)

	require.NoError(t, os.WriteFile(runnerPath, []byte("runner_id: [invalid"), 0600))
	_, _, _, err = currentDiskAcceleratorNode(ctx, runnerPath, unitPath, serverPath)
	require.ErrorContains(t, err, "reading local runner identity")
}

func TestLocalDiskAcceleratorClusterIgnoresRemoteLocalAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")
	t.Setenv("MIREN_CONFIG", filepath.Join(home, "remote.yaml"))
	t.Setenv("MIREN_SERVER_ADDRESS", "remote.example:8443")
	serverData := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(serverData, "server"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(serverData, "server", "ca.crt"), []byte("installed-ca"), 0644))
	serverConfig := filepath.Join(t.TempDir(), "server.toml")
	require.NoError(t, os.WriteFile(serverConfig, []byte("[server]\naddress = '0.0.0.0:9443'\ndata_path = '"+serverData+"'\n"), 0644))
	remote := clientconfig.NewConfig()
	remote.SetCluster("local", &clientconfig.ClusterConfig{Hostname: "remote.example:8443", CACert: "remote-ca"})
	require.NoError(t, remote.SaveTo(filepath.Join(home, "remote.yaml")))
	leafDir := filepath.Join(home, ".config/miren/clientconfig.d")
	require.NoError(t, os.MkdirAll(leafDir, 0700))
	leaf := "clusters:\n  local:\n    hostname: localhost:9555\n    ca_cert: installed-ca\n    client_cert: local-cert\n    client_key: local-key\n"
	require.NoError(t, os.WriteFile(filepath.Join(leafDir, "50-local.yaml"), []byte(leaf), 0600))
	ctx := &Context{}
	cluster, err := localDiskAcceleratorCluster(ctx, "local", serverConfig)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9555", cluster.Hostname)
	require.Equal(t, "installed-ca", cluster.CACert)
	require.Equal(t, "local-cert", cluster.ClientCert)
	require.NoError(t, os.WriteFile(serverConfig, []byte("[server]\naddress = '0.0.0.0:9443'\ndata_path = '"+serverData+"'\nconfig_cluster_name = 'this-host'\n"), 0644))
	unit := filepath.Join(t.TempDir(), "miren.service")
	require.NoError(t, os.WriteFile(unit, nil, 0644))
	_, _, inferredName, err := currentDiskAcceleratorNode(ctx, filepath.Join(home, "no-runner.yaml"), unit, serverConfig)
	require.NoError(t, err)
	require.Equal(t, "this-host", inferredName)
	cluster, err = localDiskAcceleratorCluster(ctx, inferredName, serverConfig)
	require.NoError(t, err, "the installer writes a 'local' leaf even when the server has a custom cluster name")
	require.Equal(t, "127.0.0.1:9555", cluster.Hostname)

	require.NoError(t, os.WriteFile(filepath.Join(leafDir, "50-local.yaml"), []byte(strings.Replace(leaf, "installed-ca", "remote-ca", 1)), 0600))
	_, err = localDiskAcceleratorCluster(ctx, "local", serverConfig)
	require.ErrorContains(t, err, "do not match the installed server")

	require.NoError(t, os.WriteFile(serverConfig, []byte("[server]\naddress = 'remote.example:9443'\ndata_path = '"+serverData+"'\n"), 0644))
	_, err = localDiskAcceleratorCluster(ctx, "local", serverConfig)
	require.ErrorContains(t, err, "not loopback")
}

func TestDiskAcceleratorInstallDoesNotInferIntoSelectedCluster(t *testing.T) {
	labs.EnableAll()
	err := dispatchErr(t, "disk", "accelerator", "install", "--cluster", "remote")
	require.ErrorContains(t, err, "cannot infer this host's node")
	help := captureDispatch(t, []string{"disk", "accelerator", "install", "--help"})
	require.NotContains(t, help, "--best-attempt")
	require.NotContains(t, help, "--containerd-socket")
	require.Contains(t, help, "omit to infer this host's node")
}

func TestServerInstallOffersDiskAccelerator(t *testing.T) {
	out := captureDispatch(t, []string{"server", "install", "--help"})
	require.Contains(t, out, "--disk-accelerator")
	require.Contains(t, out, "before starting the server")
}

func TestFindBundledExecutableFallsBackFromCLIOnlyRelease(t *testing.T) {
	userRelease := t.TempDir()
	systemRelease := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(userRelease, "miren"), nil, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(systemRelease, "containerd"), nil, 0755))

	path, dir := findBundledExecutable("containerd", userRelease, systemRelease)
	require.Equal(t, filepath.Join(systemRelease, "containerd"), path)
	require.Equal(t, systemRelease, dir)
}

func TestUnixSocketListening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	active, err := unixSocketListening(path)
	require.NoError(t, err)
	require.False(t, active)

	listener, err := net.Listen("unix", path)
	require.NoError(t, err)
	active, err = unixSocketListening(path)
	require.NoError(t, err)
	require.True(t, active)
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, listener.Close())
	active, err = unixSocketListening(path)
	require.NoError(t, err)
	require.False(t, active, "a stopped service may leave its socket inode behind")
}

func TestLocalDiskAcceleratorLockCoversRuntimeLifetime(t *testing.T) {
	dataPath := t.TempDir()
	unlock, err := lockLocalDiskAccelerator(dataPath)
	require.NoError(t, err)
	_, err = lockLocalDiskAccelerator(dataPath)
	require.ErrorContains(t, err, "another local accelerator installation is in progress")
	unlock()
	unlock, err = lockLocalDiskAccelerator(dataPath)
	require.NoError(t, err)
	unlock()
}

func TestSolveAndImportLbdToolchain(t *testing.T) {
	if os.Getenv("CONTAINERD_ADDRESS") == "" || os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("requires the iso containerd environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cc, err := containerd.New(containerdx.DefaultSocket)
	require.NoError(t, err)
	defer cc.Close()
	logger := slog.New(slog.DiscardHandler)
	component := buildkit.NewComponent(logger, cc, ctrbuild.DefaultNamespace, t.TempDir())
	require.NoError(t, component.Start(ctx, buildkit.Config{SocketDir: t.TempDir()}))
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Minute)
		defer stopCancel()
		require.NoError(t, component.Stop(stopCtx))
	}()
	bkc, err := component.Client(ctx)
	require.NoError(t, err)
	defer bkc.Close()

	// A small context exercises the same Dockerfile solve, OCI import and
	// unpack as the embedded toolchain without installing kernel packages.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY payload /payload\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "payload"), []byte("lbd toolchain test"), 0644))
	image := fmt.Sprintf("localhost/miren-system/lbd-builder:%s-test-%d", lbdmod.BuilderVersion(), time.Now().UnixNano())
	require.NoError(t, solveAndImportLbdToolchain(ctx, logger, bkc, cc, dir, image))
	imageCtx := namespaces.WithNamespace(ctx, ctrbuild.DefaultNamespace)
	imported, err := cc.GetImage(imageCtx, image)
	require.NoError(t, err)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		require.NoError(t, cc.ImageService().Delete(namespaces.WithNamespace(cleanupCtx, ctrbuild.DefaultNamespace), image))
	}()
	unpacked, err := imported.IsUnpacked(imageCtx, "")
	require.NoError(t, err)
	require.True(t, unpacked, "the lbd builder must be unpacked before ctrbuild can run it")

	badDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "Dockerfile"), []byte("FROM scratch\nCOPY missing /payload\n"), 0644))
	badCtx, badCancel := context.WithTimeout(ctx, 20*time.Second)
	defer badCancel()
	badImage := image + "-bad"
	require.ErrorContains(t, solveAndImportLbdToolchain(badCtx, logger, bkc, cc, badDir, badImage), "building the lbd toolchain image")
	_, err = cc.GetImage(imageCtx, badImage)
	require.Error(t, err, "a failed solve must not publish an image")
}
