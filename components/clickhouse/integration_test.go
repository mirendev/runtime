package clickhouse_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	mc "miren.dev/runtime/components/clickhouse"
	"miren.dev/runtime/pkg/testutils"
)

const testNamespace = "miren-clickhouse-test"

func TestClickHouseComponentIntegration(t *testing.T) {
	ctx := context.Background()
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err, "failed to resolve containerd client")

	// Create temporary directory for test data
	tmpDir, err := os.MkdirTemp("", "clickhouse-test")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Create logger
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create ClickHouse component
	component := mc.NewClickHouseComponent(log, cc, testNamespace, tmpDir)

	// Use test-specific ports to avoid conflicts
	httpPort := 28123
	nativePort := 29000
	interServerPort := 29009

	config := mc.ClickHouseConfig{
		HTTPPort:        httpPort,
		NativePort:      nativePort,
		InterServerPort: interServerPort,
		Database:        "default",
		User:            "default",
		Password:        "default",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Ensure cleanup
	defer func() {
		if component.IsRunning() {
			err := component.Stop(ctx)
			if err != nil {
				t.Logf("failed to stop component: %v", err)
			}
		}

		// Clean up any remaining containers
		cleanupContainer(t, cc, testNamespace)
	}()

	// Start the ClickHouse component
	t.Log("Starting ClickHouse component...")
	err = component.Start(ctx, config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err, "failed to start ClickHouse component")
	}

	// Verify component reports as running
	assert.True(t, component.IsRunning(), "component should report as running")

	// Get endpoints
	httpEndpoint := component.HTTPEndpoint()
	nativeEndpoint := component.NativeEndpoint()
	interServerEndpoint := component.InterServerEndpoint()

	assert.NotEmpty(t, httpEndpoint, "HTTP endpoint should not be empty")
	assert.NotEmpty(t, nativeEndpoint, "native endpoint should not be empty")
	assert.NotEmpty(t, interServerEndpoint, "inter-server endpoint should not be empty")

	expectedHTTPEndpoint := fmt.Sprintf("localhost:%d", httpPort)
	expectedNativeEndpoint := fmt.Sprintf("localhost:%d", nativePort)
	expectedInterServerEndpoint := fmt.Sprintf("localhost:%d", interServerPort)

	assert.Equal(t, expectedHTTPEndpoint, httpEndpoint, "HTTP endpoint should match expected")
	assert.Equal(t, expectedNativeEndpoint, nativeEndpoint, "native endpoint should match expected")
	assert.Equal(t, expectedInterServerEndpoint, interServerEndpoint, "inter-server endpoint should match expected")

	// Wait for ClickHouse to be fully ready
	t.Log("Waiting for ClickHouse to be ready...")
	time.Sleep(30 * time.Second)

	// Test HTTP endpoint availability
	t.Log("Testing HTTP endpoint...")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(httpEndpoint + "/ping")
	if err == nil {
		resp.Body.Close()
		t.Log("HTTP endpoint is responding")
	} else {
		t.Logf("HTTP endpoint test failed (may be normal during startup): %v", err)
	}

	// Test ClickHouse functionality using native protocol
	t.Log("Testing ClickHouse functionality...")
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{nativeEndpoint},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "default",
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout: 30 * time.Second,
	})

	if err != nil {
		t.Logf("Failed to connect to ClickHouse (may be still starting): %v", err)
		// Wait a bit more and try again
		time.Sleep(30 * time.Second)
		conn, err = clickhouse.Open(&clickhouse.Options{
			Addr: []string{nativeEndpoint},
			Auth: clickhouse.Auth{
				Database: "default",
				Username: "default",
				Password: "default",
			},
			Settings: clickhouse.Settings{
				"max_execution_time": 60,
			},
			DialTimeout: 30 * time.Second,
		})
	}

	require.NoError(t, err, "failed to create ClickHouse connection")
	defer conn.Close()

	// Test basic connectivity
	t.Log("Testing ClickHouse ping...")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	err = conn.Ping(ctx2)
	require.NoError(t, err, "failed to ping ClickHouse")

	// Test basic operations
	t.Log("Testing basic ClickHouse operations...")

	// Create test table
	ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel3()

	err = conn.Exec(ctx3, `
		CREATE TABLE IF NOT EXISTS test_table (
			id UInt32,
			name String,
			timestamp DateTime
		) ENGINE = Memory
	`)
	require.NoError(t, err, "failed to create test table")

	// Insert test data
	t.Log("Testing insert operation...")
	ctx4, cancel4 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel4()

	batch, err := conn.PrepareBatch(ctx4, "INSERT INTO test_table")
	require.NoError(t, err, "failed to prepare batch")

	err = batch.Append(1, "test1", time.Now())
	require.NoError(t, err, "failed to append to batch")

	err = batch.Append(2, "test2", time.Now())
	require.NoError(t, err, "failed to append to batch")

	err = batch.Send()
	require.NoError(t, err, "failed to send batch")

	// Query test data
	t.Log("Testing select operation...")
	ctx5, cancel5 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel5()

	rows, err := conn.Query(ctx5, "SELECT id, name FROM test_table ORDER BY id")
	require.NoError(t, err, "failed to query test table")

	var results []struct {
		ID   uint32
		Name string
	}

	for rows.Next() {
		var result struct {
			ID   uint32
			Name string
		}
		err := rows.Scan(&result.ID, &result.Name)
		require.NoError(t, err, "failed to scan row")
		results = append(results, result)
	}

	require.Len(t, results, 2, "expected 2 rows")
	assert.Equal(t, uint32(1), results[0].ID, "first row ID should be 1")
	assert.Equal(t, "test1", results[0].Name, "first row name should be test1")
	assert.Equal(t, uint32(2), results[1].ID, "second row ID should be 2")
	assert.Equal(t, "test2", results[1].Name, "second row name should be test2")

	// Test system tables (verify ClickHouse is working properly)
	t.Log("Testing system table access...")
	ctx6, cancel6 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel6()

	var version string
	err = conn.QueryRow(ctx6, "SELECT version()").Scan(&version)
	require.NoError(t, err, "failed to get ClickHouse version")
	assert.NotEmpty(t, version, "version should not be empty")
	t.Logf("ClickHouse version: %s", version)

	t.Log("All ClickHouse operations completed successfully!")

	// Test restart functionality
	t.Log("Testing restart functionality...")
	err = component.Stop(ctx)
	require.NoError(t, err, "failed to stop component")

	assert.False(t, component.IsRunning(), "component should not report as running after stop")

	// Start again - this should use the restart logic
	err = component.Start(ctx, config)
	require.NoError(t, err, "failed to restart ClickHouse component")

	assert.True(t, component.IsRunning(), "component should report as running after restart")

	// Wait for restart to complete
	time.Sleep(30 * time.Second)

	// Test connectivity after restart
	conn2, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{component.NativeEndpoint()},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "default",
		},
		DialTimeout: 30 * time.Second,
	})
	require.NoError(t, err, "failed to create ClickHouse connection after restart")
	defer conn2.Close()

	ctx7, cancel7 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel7()

	err = conn2.Ping(ctx7)
	require.NoError(t, err, "failed to ping ClickHouse after restart")

	t.Log("Restart test completed successfully!")
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
			es, err := task.Wait(ctx)
			require.NoError(t, err)

			select {
			case <-ctx.Done():
				require.NoError(t, ctx.Err())
			case <-es:
				//ok
			}

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
