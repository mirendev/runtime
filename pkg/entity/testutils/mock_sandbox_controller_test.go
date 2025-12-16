package testutils

import (
	"context"
	"testing"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestMockSandboxController_Basic(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := NewInMemEntityServer(t)
	defer cleanup()

	logger := TestLogger(t)

	// Create and start mock sandbox controller
	ctrl := NewMockSandboxController(logger, inmem.EAC)
	if err := ctrl.Start(ctx); err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}
	defer ctrl.Stop()

	// Create a test sandbox
	spec := CreateMinimalSandboxSpec("test-image:latest")
	sbID, err := CreateTestSandbox(ctx, inmem.EAC, "test-sandbox", spec)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Wait for sandbox to become RUNNING
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	status, err := ctrl.WaitForSandbox(waitCtx, sbID)
	if err != nil {
		t.Fatalf("Error waiting for sandbox: %v", err)
	}

	if status != compute_v1alpha.RUNNING {
		t.Errorf("Expected RUNNING status, got %s", status)
	}

	// Verify the sandbox has network and schedule info
	resp, err := inmem.EAC.Get(ctx, sbID.String())
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}

	var sb compute_v1alpha.Sandbox
	sb.Decode(resp.Entity().Entity())

	if len(sb.Network) == 0 {
		t.Error("Expected network to be assigned")
	}

	var sch compute_v1alpha.Schedule
	sch.Decode(resp.Entity().Entity())

	if sch.Key.Node == "" {
		t.Error("Expected schedule node to be assigned")
	}
}

func TestMockSandboxController_FailSandbox(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := NewInMemEntityServer(t)
	defer cleanup()

	logger := TestLogger(t)

	// Create mock sandbox controller with FailAll enabled
	ctrl := NewMockSandboxController(logger, inmem.EAC)
	ctrl.FailAll = true

	if err := ctrl.Start(ctx); err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}
	defer ctrl.Stop()

	// Create a test sandbox
	spec := CreateMinimalSandboxSpec("test-image:latest")
	sbID, err := CreateTestSandbox(ctx, inmem.EAC, "test-fail-sandbox", spec)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Wait for sandbox - should become DEAD
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	status, err := ctrl.WaitForSandbox(waitCtx, sbID)
	if err != nil {
		t.Fatalf("Error waiting for sandbox: %v", err)
	}

	if status != compute_v1alpha.DEAD {
		t.Errorf("Expected DEAD status, got %s", status)
	}
}

func TestMockSandboxController_StartupDelay(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := NewInMemEntityServer(t)
	defer cleanup()

	logger := TestLogger(t)

	// Create mock sandbox controller with delay
	ctrl := NewMockSandboxController(logger, inmem.EAC)
	ctrl.StartupDelay = 100 * time.Millisecond

	if err := ctrl.Start(ctx); err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}
	defer ctrl.Stop()

	// Create a test sandbox
	spec := CreateMinimalSandboxSpec("test-image:latest")
	sbID, err := CreateTestSandbox(ctx, inmem.EAC, "test-delay-sandbox", spec)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	startTime := time.Now()

	// Wait for sandbox to become RUNNING
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	status, err := ctrl.WaitForSandbox(waitCtx, sbID)
	if err != nil {
		t.Fatalf("Error waiting for sandbox: %v", err)
	}

	elapsed := time.Since(startTime)

	if status != compute_v1alpha.RUNNING {
		t.Errorf("Expected RUNNING status, got %s", status)
	}

	// Verify the delay was applied (with some tolerance)
	if elapsed < 90*time.Millisecond {
		t.Errorf("Expected at least 90ms delay, got %v", elapsed)
	}
}

func TestMockSandboxController_Callback(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := NewInMemEntityServer(t)
	defer cleanup()

	logger := TestLogger(t)

	// Track callbacks
	readyCalled := make(chan struct{}, 1)

	// Create mock sandbox controller with callback
	ctrl := NewMockSandboxController(logger, inmem.EAC)
	ctrl.OnSandboxReady = func(_ entity.Id) {
		readyCalled <- struct{}{}
	}

	if err := ctrl.Start(ctx); err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}
	defer ctrl.Stop()

	// Create a test sandbox
	spec := CreateMinimalSandboxSpec("test-image:latest")
	_, err := CreateTestSandbox(ctx, inmem.EAC, "test-callback-sandbox", spec)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Wait for callback
	select {
	case <-readyCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("OnSandboxReady callback was not called")
	}
}
