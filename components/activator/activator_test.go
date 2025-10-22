package activator

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// Test lease operations
func TestActivatorLeaseOperations(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	testVer := &core_v1alpha.AppVersion{
		ID:       entity.Id("ver-1"),
		App:      entity.Id("app-1"),
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
						ScaleDownDelay:      "15m",
					},
				},
			},
		},
	}

	ent := entity.Blank()
	ent.SetID("sb-1")

	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	})
	tracker := strategy.InitializeTracker()

	testSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sb-1"),
			Status: compute_v1alpha.RUNNING,
		},
		ent:         ent,
		lastRenewal: time.Now(),
		url:         "http://localhost:3000",
		tracker:     tracker,
	}

	activator := &localActivator{
		log: log,
		versions: map[verKey]*verSandboxes{
			{"ver-1", "web"}: {
				ver:       testVer,
				sandboxes: []*sandbox{testSandbox},
				strategy:  strategy,
			},
		},
	}

	// Test ReleaseLease
	lease := &Lease{
		ver:     testVer,
		sandbox: testSandbox.sandbox,
		pool:    "default",
		service: "web",
		Size:    2,
	}

	// Acquire slots
	tracker.AcquireLease()

	// Release lease
	err := activator.ReleaseLease(context.Background(), lease)
	require.NoError(t, err)

	// Verify slots were released
	assert.Equal(t, 0, tracker.Used())
}

// Test concurrent access safety
func TestActivatorConcurrentSafety(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	activator := &localActivator{
		log:      log,
		versions: make(map[verKey]*verSandboxes),
	}

	// Run multiple goroutines accessing the versions map
	done := make(chan bool, 3)

	// Goroutine 1: Add versions
	go func() {
		for i := 0; i < 100; i++ {
			activator.mu.Lock()
			activator.versions[verKey{ver: "ver-1", service: "web"}] = &verSandboxes{
				sandboxes: []*sandbox{},
			}
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Goroutine 2: Read versions
	go func() {
		for i := 0; i < 100; i++ {
			activator.mu.Lock()
			_ = activator.versions[verKey{ver: "ver-1", service: "web"}]
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Goroutine 3: Delete versions
	go func() {
		for i := 0; i < 100; i++ {
			activator.mu.Lock()
			delete(activator.versions, verKey{ver: "ver-1", service: "web"})
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Test passed if no race condition occurred
}

// Test activator sandbox recovery with real entity server
func TestActivatorRecoverSandboxesWithEntityServer(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version
	appVer := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 8080,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create sandbox entity
	sb := compute_v1alpha.Sandbox{
		Version: appVer.ID,
		Status:  compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.100"},
		},
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name:   "test-sandbox",
			Labels: types.LabelSet("service", "web"),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/test-sb"),
		sb.Encode,
	).Attrs())

	pr, err := server.EAC.Put(ctx, &rpcE)
	require.NoError(t, err)
	sb.ID = entity.Id(pr.Id())

	// Create activator and trigger recovery
	log := testutils.TestLogger(t)
	activator := &localActivator{
		log:      log,
		eac:      server.EAC,
		versions: make(map[verKey]*verSandboxes),
		pools:    make(map[verKey]*compute_v1alpha.SandboxPool),
	}

	err = activator.recoverSandboxes(ctx)
	require.NoError(t, err)

	// Verify sandbox was recovered
	key := verKey{appVer.ID.String(), "web"}
	vs, ok := activator.versions[key]
	require.True(t, ok, "version should be in map")
	require.Len(t, vs.sandboxes, 1, "should have recovered 1 running sandbox")

	// Verify sandbox details
	recoveredSb := vs.sandboxes[0]
	assert.Equal(t, sb.ID, recoveredSb.sandbox.ID)
	assert.Equal(t, compute_v1alpha.RUNNING, recoveredSb.sandbox.Status)
	assert.Equal(t, "http://10.0.0.100:8080", recoveredSb.url)
	assert.Equal(t, 10, recoveredSb.tracker.Max())
	assert.Equal(t, 0, recoveredSb.tracker.Used())
	assert.WithinDuration(t, time.Now(), recoveredSb.lastRenewal, 5*time.Second)
}

// TestActivatorRecoveryIntegration tests the full activator recovery scenario
func TestActivatorRecoveryIntegration(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server for testing
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	client := es.Client

	// Create test app and version
	app := &core_v1alpha.App{}
	appID, err := client.Create(ctx, "integration-app", app)
	require.NoError(t, err)
	app.ID = appID

	appVer := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "api",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 20,
					},
				},
			},
		},
	}
	verID, err := client.Create(ctx, "integration-ver", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create multiple running sandboxes
	for i := 0; i < 3; i++ {
		sb := compute_v1alpha.Sandbox{
			Version: appVer.ID,
			Status:  compute_v1alpha.RUNNING,
			Network: []compute_v1alpha.Network{
				{Address: "10.0.0.100/32"},
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			(&core_v1alpha.Metadata{
				Name:   "sandbox-" + string(rune('a'+i)),
				Labels: types.LabelSet("service", "api"),
			}).Encode,
			entity.Ident, entity.MustKeyword("sandbox/sb-"+string(rune('a'+i))),
			sb.Encode,
		).Attrs())

		_, err := es.EAC.Put(ctx, &rpcE)
		require.NoError(t, err)
	}

	// Create activator - should recover all sandboxes
	log := testutils.TestLogger(t)
	activator := NewLocalActivator(ctx, log, es.EAC).(*localActivator)

	// Give a moment for recovery to complete
	time.Sleep(100 * time.Millisecond)

	// Verify recovery
	key := verKey{appVer.ID.String(), "api"}
	vs, ok := activator.versions[key]
	require.True(t, ok, "version should be tracked")
	assert.Len(t, vs.sandboxes, 3, "should recover all 3 sandboxes")

	// Verify strategy configuration
	assert.Equal(t, 0, vs.strategy.DesiredInstances()) // Auto mode scales to zero
}
