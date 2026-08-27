package etcd_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/testutils"
)

func TestEtcdComponentIntegration(t *testing.T) {
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	cc := testDeps.CC

	// Create temporary directory for test data
	tmpDir, err := os.MkdirTemp("", "etcd-test")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Create logger
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Use dynamic namespace to avoid conflicts with parallel tests
	testNamespace := fmt.Sprintf("miren-etcd-test-%d", time.Now().UnixNano())

	// Create etcd component
	component := etcd.NewEtcdComponent(log, cc, testNamespace, tmpDir)

	// Use dynamic ports to avoid conflicts with parallel tests
	clientPort := testutils.GetFreePort(t)
	httpClientPort := testutils.GetFreePort(t)
	peerPort := testutils.GetFreePort(t)

	config := etcd.EtcdConfig{
		Name:           "test-etcd",
		ClientPort:     clientPort,
		HTTPClientPort: httpClientPort,
		PeerPort:       peerPort,
		InitialToken:   "test-cluster",
		ClusterState:   "new",
	}

	// Ensure cleanup
	defer func() {
		if component.IsRunning() {
			err := component.Stop(context.Background())
			if err != nil {
				t.Logf("failed to stop component: %v", err)
			}
		}

		// Clean up any remaining containers
		cleanupContainer(t, cc, testNamespace)
	}()

	// Start the etcd component
	t.Log("Starting etcd component...")
	err = component.Start(context.Background(), config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err, "failed to start etcd component")
	}

	// Verify component reports as running
	assert.True(t, component.IsRunning(), "component should report as running")

	// Get client endpoint
	endpoint := component.ClientEndpoint()
	assert.NotEmpty(t, endpoint, "client endpoint should not be empty")

	expectedEndpoint := fmt.Sprintf("http://localhost:%d", clientPort)
	assert.Equal(t, expectedEndpoint, endpoint, "client endpoint should match expected")

	// Wait for etcd to be fully ready by polling
	t.Log("Waiting for etcd to be ready...")
	var etcdClient *clientv3.Client
	require.Eventually(t, func() bool {
		var err error
		etcdClient, err = clientv3.New(clientv3.Config{
			Endpoints:   []string{endpoint},
			DialTimeout: 1 * time.Second,
		})
		if err != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err = etcdClient.Get(ctx, "health-check")
		if err != nil {
			etcdClient.Close()
			etcdClient = nil
			return false
		}
		return true
	}, 30*time.Second, 500*time.Millisecond, "etcd failed to become ready")
	defer etcdClient.Close()

	t.Log("Testing etcd functionality...")

	// Test basic key-value operations
	testKey := "test-key"
	testValue := "test-value"

	// Put operation
	t.Log("Testing Put operation...")
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()

	_, err = etcdClient.Put(ctx3, testKey, testValue)
	require.NoError(t, err, "failed to put key-value")

	// Get operation
	t.Log("Testing Get operation...")
	ctx4, cancel4 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel4()

	resp, err := etcdClient.Get(ctx4, testKey)
	require.NoError(t, err, "failed to get key")
	require.Len(t, resp.Kvs, 1, "expected 1 key-value pair")
	assert.Equal(t, testValue, string(resp.Kvs[0].Value), "value should match")

	// Delete operation
	t.Log("Testing Delete operation...")
	ctx5, cancel5 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel5()

	_, err = etcdClient.Delete(ctx5, testKey)
	require.NoError(t, err, "failed to delete key")

	// Verify deletion
	ctx6, cancel6 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel6()

	resp, err = etcdClient.Get(ctx6, testKey)
	require.NoError(t, err, "failed to get key after deletion")
	assert.Len(t, resp.Kvs, 0, "expected 0 key-value pairs after deletion")

	// Test watch functionality
	t.Log("Testing Watch operation...")
	watchKey := "watch-test"
	watchValue := "watch-value"

	ctx7, cancel7 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel7()

	watchCh := etcdClient.Watch(ctx7, watchKey)

	// Put a value in a goroutine to trigger the watch
	go func() {
		time.Sleep(1 * time.Second)
		ctx8, cancel8 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel8()
		etcdClient.Put(ctx8, watchKey, watchValue)
	}()

	// Wait for watch event
	select {
	case watchResp := <-watchCh:
		require.NoError(t, watchResp.Err(), "watch should not error")
		require.Len(t, watchResp.Events, 1, "expected 1 watch event")
		assert.Equal(t, watchValue, string(watchResp.Events[0].Kv.Value), "watch value should match")
	case <-ctx7.Done():
		t.Fatal("watch operation timed out")
	}

	t.Log("All etcd operations completed successfully!")

	// Test restart functionality
	t.Log("Testing restart functionality...")
	err = component.Stop(context.Background())
	require.NoError(t, err, "failed to stop component")

	assert.False(t, component.IsRunning(), "component should not report as running after stop")

	// Start again - this should use the restart logic
	err = component.Start(context.Background(), config)
	require.NoError(t, err, "failed to restart etcd component")

	assert.True(t, component.IsRunning(), "component should report as running after restart")

	// Wait for etcd to be ready after restart by polling
	var etcdClient2 *clientv3.Client
	require.Eventually(t, func() bool {
		var err error
		etcdClient2, err = clientv3.New(clientv3.Config{
			Endpoints:   []string{component.ClientEndpoint()},
			DialTimeout: 1 * time.Second,
		})
		if err != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err = etcdClient2.Get(ctx, "restart-test")
		if err != nil {
			etcdClient2.Close()
			etcdClient2 = nil
			return false
		}
		return true
	}, 30*time.Second, 500*time.Millisecond, "etcd failed to become ready after restart")
	defer etcdClient2.Close()

	t.Log("Restart test completed successfully!")
}

func TestEtcdComponentAutoRestart(t *testing.T) {
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	cc := testDeps.CC

	// Create temporary directory for test data
	tmpDir, err := os.MkdirTemp("", "etcd-restart-test")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Create logger
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Use dynamic namespace to avoid conflicts with parallel tests
	testNamespace := fmt.Sprintf("miren-etcd-restart-test-%d", time.Now().UnixNano())

	// Create etcd component
	component := etcd.NewEtcdComponent(log, cc, testNamespace, tmpDir)

	// Use dynamic ports to avoid conflicts
	clientPort := testutils.GetFreePort(t)
	peerPort := testutils.GetFreePort(t)
	httpClientPort := testutils.GetFreePort(t)

	config := etcd.EtcdConfig{
		Name:           "test-etcd-restart",
		ClientPort:     clientPort,
		HTTPClientPort: httpClientPort,
		PeerPort:       peerPort,
		InitialToken:   "restart-test-cluster",
		ClusterState:   "new",
	}

	// Ensure cleanup
	defer func() {
		if component.IsRunning() {
			err := component.Stop(context.Background())
			if err != nil {
				t.Logf("failed to stop component: %v", err)
			}
		}
		cleanupContainer(t, cc, testNamespace)
	}()

	// Start the etcd component
	t.Log("Starting etcd component...")
	err = component.Start(context.Background(), config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err, "failed to start etcd component")
	}

	assert.True(t, component.IsRunning(), "component should report as running")

	// Wait for etcd to be fully ready
	endpoint := component.ClientEndpoint()
	var etcdClient *clientv3.Client
	require.Eventually(t, func() bool {
		var err error
		etcdClient, err = clientv3.New(clientv3.Config{
			Endpoints:   []string{endpoint},
			DialTimeout: 1 * time.Second,
		})
		if err != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err = etcdClient.Get(ctx, "health-check")
		if err != nil {
			etcdClient.Close()
			etcdClient = nil
			return false
		}
		return true
	}, 30*time.Second, 500*time.Millisecond, "etcd failed to become ready")
	etcdClient.Close()

	t.Log("etcd is ready, now killing the task to simulate crash...")

	// Kill the etcd task directly using containerd to simulate a crash
	ctx := namespaces.WithNamespace(context.Background(), testNamespace)
	containers, err := cc.Containers(ctx)
	require.NoError(t, err, "failed to list containers")

	var etcdContainer containerd.Container
	for _, c := range containers {
		if strings.Contains(c.ID(), "etcd") {
			etcdContainer = c
			break
		}
	}
	require.NotNil(t, etcdContainer, "should find etcd container")

	task, err := etcdContainer.Task(ctx, nil)
	require.NoError(t, err, "should get task")

	// Kill the task with SIGKILL to simulate crash
	err = task.Kill(ctx, unix.SIGKILL)
	require.NoError(t, err, "should be able to kill task")

	t.Log("Task killed, waiting for auto-restart...")

	// Wait for the component to auto-restart and become ready again
	// The restart has a 2 second backoff for the first restart
	require.Eventually(t, func() bool {
		// Try to connect to etcd
		client, err := clientv3.New(clientv3.Config{
			Endpoints:   []string{endpoint},
			DialTimeout: 3 * time.Second,
		})
		if err != nil {
			return false
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err = client.Get(ctx, "restart-health-check")
		return err == nil
	}, 90*time.Second, 1*time.Second, "etcd should auto-restart and become ready")

	t.Log("etcd auto-restarted successfully!")

	// Verify the component still reports as running
	assert.True(t, component.IsRunning(), "component should still report as running after auto-restart")

	// Test that we can still perform operations
	etcdClient2, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	})
	require.NoError(t, err, "should be able to create client after restart")
	defer etcdClient2.Close()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	_, err = etcdClient2.Put(ctx2, "post-restart-key", "post-restart-value")
	require.NoError(t, err, "should be able to write after auto-restart")

	resp, err := etcdClient2.Get(ctx2, "post-restart-key")
	require.NoError(t, err, "should be able to read after auto-restart")
	require.Len(t, resp.Kvs, 1, "should have one key")
	assert.Equal(t, "post-restart-value", string(resp.Kvs[0].Value), "value should match")

	t.Log("Auto-restart test completed successfully!")
}

// TestEtcdRecreateAfterUncleanShutdown is the regression test for MIR-1463.
//
// Enabling distributed runners flips embedded etcd from plaintext to TLS, which makes
// the component recreate its container on the next boot. If the *previous* server died
// uncleanly, the etcd containerd task is left registered but its init process is gone.
// The old cleanup path sent SIGTERM, got "not found", and bailed without deleting the
// task — so the container couldn't be deleted, its snapshot leaked, and creating the new
// container failed with "snapshot already exists", wedging the node.
//
// We reproduce the wedge condition directly: a leftover "miren-etcd" container that owns
// the "miren-etcd-snapshot" snapshot, with a registered-but-dead task, plus a state file
// from the prior boot. We force the recreate with an old config_version rather than a real
// TLS flip — it's the same CleanupExistingContainer path (the actual site of the bug) and
// keeps the test hermetic (no CA seeding or mTLS client needed). The assertion is that the
// recreate succeeds and etcd comes back up serving reads and writes.
func TestEtcdRecreateAfterUncleanShutdown(t *testing.T) {
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	cc := testDeps.CC

	tmpDir, err := os.MkdirTemp("", "etcd-recreate-test")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	testNamespace := fmt.Sprintf("miren-etcd-recreate-test-%d", time.Now().UnixNano())
	ctx := namespaces.WithNamespace(context.Background(), testNamespace)

	// Backstop cleanup for both the seeded and recreated containers.
	defer cleanupContainer(t, cc, testNamespace)

	// Seed the leftover container + snapshot exactly as a real plaintext boot would.
	t.Log("Seeding leftover etcd container from a prior boot...")
	image, err := cc.Pull(ctx, imagerefs.Etcd, containerd.WithPullUnpack)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err, "failed to pull etcd image")
	}

	container, err := cc.NewContainer(ctx, "miren-etcd",
		containerd.WithImage(image),
		containerd.WithNewSnapshot("miren-etcd-snapshot", image),
		containerd.WithNewSpec(
			oci.WithImageConfig(image),
			oci.WithProcessArgs("/usr/local/bin/etcd", "--version"),
		),
	)
	require.NoError(t, err, "failed to seed leftover etcd container")

	// Start a task and let it exit on its own, but deliberately never delete it. This
	// mimics an unclean shutdown: the containerd task record survives while its init
	// process is gone, so a later SIGTERM comes back "not found".
	task, err := container.NewTask(ctx, cio.NullIO)
	require.NoError(t, err, "failed to create seed task")
	exitCh, err := task.Wait(ctx)
	require.NoError(t, err, "failed to wait on seed task")
	require.NoError(t, task.Start(ctx), "failed to start seed task")
	select {
	case <-exitCh:
	case <-time.After(30 * time.Second):
		t.Fatal("seed task did not exit in time")
	}

	// Confirm we actually reproduced the wedge precondition: the task is still
	// registered, but its process is gone so it can't be signalled.
	leftover, err := container.Task(ctx, nil)
	require.NoError(t, err, "leftover task should still be registered after an unclean shutdown")
	require.Error(t, leftover.Kill(ctx, unix.SIGTERM), "leftover task process should already be gone")

	// Write the state file from the prior boot so Start takes the recreate path.
	etcdDir := filepath.Join(tmpDir, "etcd")
	require.NoError(t, os.MkdirAll(etcdDir, 0700), "failed to create etcd data dir")
	require.NoError(t, os.WriteFile(
		filepath.Join(etcdDir, "etcd-state.json"),
		[]byte(`{"tls_enabled":false,"config_version":1}`),
		0600,
	), "failed to write seed state file")

	// Now perform the recreate that used to wedge on the leaked snapshot.
	t.Log("Starting etcd component (should recreate cleanly, not wedge)...")
	component := etcd.NewEtcdComponent(log, cc, testNamespace, tmpDir)

	clientPort := testutils.GetFreePort(t)
	peerPort := testutils.GetFreePort(t)
	httpClientPort := testutils.GetFreePort(t)

	config := etcd.EtcdConfig{
		Name:           "test-etcd-recreate",
		ClientPort:     clientPort,
		HTTPClientPort: httpClientPort,
		PeerPort:       peerPort,
		InitialToken:   "recreate-test-cluster",
		ClusterState:   "new",
	}

	defer func() {
		if component.IsRunning() {
			if err := component.Stop(context.Background()); err != nil {
				t.Logf("failed to stop component: %v", err)
			}
		}
	}()

	err = component.Start(context.Background(), config)
	if err != nil && strings.Contains(err.Error(), "permission denied") {
		t.Skip("permission denied error, skipping test")
	}
	require.NoError(t, err, "recreate after an unclean shutdown must not wedge on a leaked snapshot")
	require.True(t, component.IsRunning(), "component should be running after a clean recreate")

	// Verify etcd actually serves after the recreate.
	endpoint := component.ClientEndpoint()
	var etcdClient *clientv3.Client
	require.Eventually(t, func() bool {
		etcdClient, err = clientv3.New(clientv3.Config{
			Endpoints:   []string{endpoint},
			DialTimeout: 1 * time.Second,
		})
		if err != nil {
			return false
		}
		checkCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err = etcdClient.Get(checkCtx, "health-check")
		if err != nil {
			etcdClient.Close()
			etcdClient = nil
			return false
		}
		return true
	}, 30*time.Second, 500*time.Millisecond, "recreated etcd failed to become ready")
	defer etcdClient.Close()

	opCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = etcdClient.Put(opCtx, "recreate-key", "recreate-value")
	require.NoError(t, err, "should be able to write after recreate")
	resp, err := etcdClient.Get(opCtx, "recreate-key")
	require.NoError(t, err, "should be able to read after recreate")
	require.Len(t, resp.Kvs, 1, "expected one key after recreate")
	assert.Equal(t, "recreate-value", string(resp.Kvs[0].Value), "value should round-trip after recreate")

	t.Log("Recreate after unclean shutdown succeeded!")
}

func cleanupContainer(t *testing.T, cc *containerd.Client, namespace string) {
	ctx := context.Background()
	ctx = namespaces.WithNamespace(ctx, namespace)

	// Try to find and delete any test containers
	containers, err := cc.Containers(ctx)
	if err != nil {
		t.Logf("failed to list containers for cleanup: %v", err)
		return
	}

	for _, container := range containers {
		// Stop and delete task if it exists
		task, err := container.Task(ctx, nil)
		if err == nil {
			task.Kill(ctx, unix.SIGTERM)
			task.Wait(ctx)
			task.Delete(ctx)
		}

		// Delete container
		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			t.Logf("failed to delete container %s: %v", container.ID(), err)
		} else {
			t.Logf("cleaned up container %s", container.ID())
		}
	}
}
