package execproxy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

func TestBuildSandboxSpec(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)

	server := &Server{
		Log: logger,
		EAC: inmem.EAC,
	}

	tests := []struct {
		name          string
		app           *core_v1alpha.App
		ver           *core_v1alpha.AppVersion
		expectedImage string
		expectedEnvs  []string
	}{
		{
			name: "basic spec with global config",
			app: &core_v1alpha.App{
				ID: "app/basic-app",
			},
			ver: &core_v1alpha.AppVersion{
				ID:       "version/basic-v1",
				App:      "app/basic-app",
				Version:  "v1",
				ImageUrl: "basic-image:latest",
				Config: core_v1alpha.Config{
					StartDirectory: "/workspace",
					Variable: []core_v1alpha.Variable{
						{Key: "GLOBAL_VAR", Value: "global_value"},
					},
				},
			},
			expectedImage: "basic-image:latest",
			expectedEnvs:  []string{"GLOBAL_VAR=global_value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create app metadata entity
			_, err := inmem.EAC.Create(ctx, entity.New(
				(&core_v1alpha.Metadata{
					Name: tt.app.ID.String()[4:], // Remove "app/" prefix
				}).Encode,
				entity.DBId, tt.app.ID,
				tt.app.Encode,
			).Attrs())
			require.NoError(t, err, "Failed to create app entity")

			spec, err := server.buildSandboxSpec(ctx, tt.app, tt.ver)
			require.NoError(t, err, "buildSandboxSpec failed")
			require.NotNil(t, spec, "Expected spec to be returned")

			assert.Equal(t, tt.ver.ID, spec.Version)
			require.Len(t, spec.Container, 1, "Expected 1 container")

			container := spec.Container[0]

			if tt.expectedImage != "" {
				assert.Equal(t, tt.expectedImage, container.Image)
			}

			// Check expected env vars are present
			for _, expectedEnv := range tt.expectedEnvs {
				assert.Contains(t, container.Env, expectedEnv)
			}

			// Always uses /bin/sh with TTY for exec
			assert.Equal(t, "/bin/sh", container.Command)
			assert.True(t, container.Tty, "Expected Tty=true")
			assert.True(t, container.Stdin, "Expected Stdin=true")
		})
	}
}

func TestCreateEphemeralSandbox(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)

	server := &Server{
		Log: logger,
		EAC: inmem.EAC,
	}

	// Start mock sandbox controller
	mockCtrl := testutils.NewMockSandboxController(logger, inmem.EAC)
	require.NoError(t, mockCtrl.Start(ctx), "Failed to start mock sandbox controller")
	defer mockCtrl.Stop()

	app := &core_v1alpha.App{
		ID: "app/test-app",
	}

	ver := &core_v1alpha.AppVersion{
		ID:       "version/test-v1",
		App:      "app/test-app",
		Version:  "v1",
		ImageUrl: "test-image:latest",
		Config: core_v1alpha.Config{
			StartDirectory: "/app",
			Variable: []core_v1alpha.Variable{
				{Key: "ENV", Value: "test"},
			},
		},
	}

	// Create app metadata entity
	_, err := inmem.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: "test-app",
		}).Encode,
		entity.DBId, app.ID,
		app.Encode,
	).Attrs())
	require.NoError(t, err, "Failed to create app entity")

	// Create ephemeral sandbox - should succeed with mock controller
	sbEnt, cleanupFn, createErr := server.createEphemeralSandbox(ctx, app, ver)
	require.NoError(t, createErr, "createEphemeralSandbox failed")
	defer cleanupFn()

	// Verify sandbox was created and is RUNNING
	require.NotNil(t, sbEnt, "Expected sandbox entity to be returned")

	var sb compute_v1alpha.Sandbox
	sb.Decode(sbEnt)

	assert.Equal(t, compute_v1alpha.RUNNING, sb.Status)

	// Verify sandbox has expected properties
	require.Len(t, sb.Spec.Container, 1, "Expected 1 container")
	assert.Equal(t, "test-image:latest", sb.Spec.Container[0].Image)
}

func TestCreateEphemeralSandbox_Timeout(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)

	server := &Server{
		Log: logger,
		EAC: inmem.EAC,
	}

	// Do NOT start mock controller - sandbox will never become RUNNING

	app := &core_v1alpha.App{
		ID: "app/timeout-app",
	}

	ver := &core_v1alpha.AppVersion{
		ID:       "version/timeout-v1",
		App:      "app/timeout-app",
		Version:  "v1",
		ImageUrl: "test-image:latest",
	}

	// Create app metadata entity
	_, err := inmem.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: "timeout-app",
		}).Encode,
		entity.DBId, app.ID,
		app.Encode,
	).Attrs())
	require.NoError(t, err, "Failed to create app entity")

	// Create a context with short timeout
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// This will timeout because there's no sandbox controller
	_, _, createErr := server.createEphemeralSandbox(shortCtx, app, ver)
	require.Error(t, createErr, "Expected error due to timeout")

	// Verify the error is a timeout error
	assert.True(t,
		assert.ObjectsAreEqual(true, contains(createErr.Error(), "timeout")) ||
			assert.ObjectsAreEqual(true, contains(createErr.Error(), "sandbox failed to start")),
		"Unexpected error: %v", createErr)
}

func TestCreateEphemeralSandbox_Failure(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)

	server := &Server{
		Log: logger,
		EAC: inmem.EAC,
	}

	// Start mock sandbox controller that fails all sandboxes
	mockCtrl := testutils.NewMockSandboxController(logger, inmem.EAC)
	mockCtrl.FailAll = true
	require.NoError(t, mockCtrl.Start(ctx), "Failed to start mock sandbox controller")
	defer mockCtrl.Stop()

	app := &core_v1alpha.App{
		ID: "app/fail-app",
	}

	ver := &core_v1alpha.AppVersion{
		ID:       "version/fail-v1",
		App:      "app/fail-app",
		Version:  "v1",
		ImageUrl: "test-image:latest",
	}

	// Create app metadata entity
	_, err := inmem.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: "fail-app",
		}).Encode,
		entity.DBId, app.ID,
		app.Encode,
	).Attrs())
	require.NoError(t, err, "Failed to create app entity")

	// This should fail because sandbox transitions to DEAD
	_, _, createErr := server.createEphemeralSandbox(ctx, app, ver)
	require.Error(t, createErr, "Expected error for failed sandbox")

	// Verify the error mentions the dead status
	assert.True(t,
		contains(createErr.Error(), "dead") || contains(createErr.Error(), "failed"),
		"Expected error to mention failure, got: %v", createErr)
}

func TestWaitForSandboxRunning(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)

	server := &Server{
		Log: logger,
		EAC: inmem.EAC,
	}

	t.Run("sandbox becomes running", func(t *testing.T) {
		sbID := entity.Id("sandbox/test-running")

		// Create a sandbox that's already RUNNING
		sb := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.RUNNING,
			Spec: compute_v1alpha.SandboxSpec{
				Container: []compute_v1alpha.SandboxSpecContainer{
					{Name: "app", Image: "test:latest"},
				},
			},
		}

		_, err := inmem.EAC.Create(ctx, entity.New(
			(&core_v1alpha.Metadata{
				Name: "test-running",
			}).Encode,
			entity.DBId, sbID,
			sb.Encode,
		).Attrs())
		require.NoError(t, err, "Failed to create sandbox")

		// Should find it immediately
		result, err := server.waitForSandboxRunning(ctx, sbID)
		require.NoError(t, err, "waitForSandboxRunning failed")
		require.NotNil(t, result, "Expected sandbox entity to be returned")
	})

	t.Run("sandbox is dead", func(t *testing.T) {
		sbID := entity.Id("sandbox/test-dead")

		// Create a sandbox that's DEAD
		sb := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.DEAD,
		}

		_, err := inmem.EAC.Create(ctx, entity.New(
			(&core_v1alpha.Metadata{
				Name: "test-dead",
			}).Encode,
			entity.DBId, sbID,
			sb.Encode,
		).Attrs())
		require.NoError(t, err, "Failed to create sandbox")

		// Should return error for dead sandbox
		_, err = server.waitForSandboxRunning(ctx, sbID)
		require.Error(t, err, "Expected error for dead sandbox")
		assert.Contains(t, err.Error(), "dead")
	})

	t.Run("timeout waiting for sandbox", func(t *testing.T) {
		sbID := entity.Id("sandbox/test-pending")

		// Create a sandbox that's PENDING (will never become RUNNING in test)
		sb := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.PENDING,
		}

		_, err := inmem.EAC.Create(ctx, entity.New(
			(&core_v1alpha.Metadata{
				Name: "test-pending",
			}).Encode,
			entity.DBId, sbID,
			sb.Encode,
		).Attrs())
		require.NoError(t, err, "Failed to create sandbox")

		// Use short timeout
		shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		_, err = server.waitForSandboxRunning(shortCtx, sbID)
		require.Error(t, err, "Expected timeout error")
		assert.Contains(t, err.Error(), "timeout")
	})
}

func TestDeleteSandbox(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)

	server := &Server{
		Log: logger,
		EAC: inmem.EAC,
	}

	sbID := entity.Id("sandbox/test-delete")

	// Create a sandbox
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
	}

	_, err := inmem.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: "test-delete",
			Labels: types.LabelSet(
				"ephemeral", "true",
			),
		}).Encode,
		entity.DBId, sbID,
		sb.Encode,
	).Attrs())
	require.NoError(t, err, "Failed to create sandbox")

	// Verify it exists
	_, err = inmem.EAC.Get(ctx, sbID.String())
	require.NoError(t, err, "Sandbox should exist")

	// Delete it
	server.deleteSandbox(sbID)

	// Verify it's gone
	_, err = inmem.EAC.Get(ctx, sbID.String())
	assert.Error(t, err, "Sandbox should have been deleted")
}

// contains checks if substr is in s
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
