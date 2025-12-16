package testutils

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

// MockSandboxController simulates a sandbox controller for testing.
// It watches for PENDING sandboxes and transitions them to RUNNING (or DEAD if configured).
type MockSandboxController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// PollInterval controls how often the controller checks for pending sandboxes.
	// Defaults to 50ms if not set.
	PollInterval time.Duration

	// StartupDelay is the delay before transitioning a sandbox from PENDING to RUNNING.
	// Defaults to 0 (immediate transition).
	StartupDelay time.Duration

	// FailSandboxes is a set of sandbox IDs that should transition to DEAD instead of RUNNING.
	FailSandboxes map[entity.Id]bool

	// FailAll causes all sandboxes to transition to DEAD.
	FailAll bool

	// OnSandboxReady is called when a sandbox transitions to RUNNING.
	// Can be used for test synchronization.
	OnSandboxReady func(id entity.Id)

	// OnSandboxFailed is called when a sandbox transitions to DEAD.
	OnSandboxFailed func(id entity.Id)

	// AssignNetwork controls whether to assign a mock network address.
	// Defaults to true.
	AssignNetwork bool

	// NodeID is the mock node ID to assign to sandboxes.
	// Defaults to "node/mock-node".
	NodeID entity.Id

	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	wg        sync.WaitGroup
	processed map[entity.Id]bool
}

// NewMockSandboxController creates a new mock sandbox controller with default settings.
func NewMockSandboxController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *MockSandboxController {
	return &MockSandboxController{
		Log:           log,
		EAC:           eac,
		PollInterval:  50 * time.Millisecond,
		AssignNetwork: true,
		NodeID:        entity.Id("node/mock-node"),
		FailSandboxes: make(map[entity.Id]bool),
		processed:     make(map[entity.Id]bool),
	}
}

// Start begins the mock sandbox controller in a background goroutine.
// Call Stop() to shut it down.
func (c *MockSandboxController) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	// Ensure the mock node exists
	if err := c.ensureNode(ctx); err != nil {
		return err
	}

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.running = true
	c.wg.Add(1)

	go c.run()

	return nil
}

// Stop shuts down the mock sandbox controller and waits for it to finish.
func (c *MockSandboxController) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.cancel()
	c.running = false
	c.mu.Unlock()

	c.wg.Wait()
}

// Reset clears the processed sandbox tracking, allowing sandboxes to be processed again.
func (c *MockSandboxController) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.processed = make(map[entity.Id]bool)
}

// MarkFailed adds a sandbox ID to the list of sandboxes that should fail.
func (c *MockSandboxController) MarkFailed(id entity.Id) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.FailSandboxes[id] = true
}

func (c *MockSandboxController) ensureNode(ctx context.Context) error {
	node := &compute_v1alpha.Node{
		ID:         c.NodeID,
		ApiAddress: "mock://localhost:0",
	}

	_, err := c.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: "mock-node",
		}).Encode,
		entity.DBId, c.NodeID,
		node.Encode,
	).Attrs())
	if err != nil {
		// Ignore if already exists
		c.Log.Debug("node creation result", "error", err)
	}

	return nil
}

func (c *MockSandboxController) run() {
	defer c.wg.Done()

	pollInterval := c.PollInterval
	if pollInterval == 0 {
		pollInterval = 50 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.processPendingSandboxes()
		}
	}
}

func (c *MockSandboxController) processPendingSandboxes() {
	ctx := c.ctx

	// List all sandboxes
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		c.Log.Debug("failed to list sandboxes", "error", err)
		return
	}

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Skip if not pending
		if sb.Status != compute_v1alpha.PENDING {
			continue
		}

		// Skip if already processed
		c.mu.Lock()
		if c.processed[sb.ID] {
			c.mu.Unlock()
			continue
		}
		c.processed[sb.ID] = true
		shouldFail := c.FailAll || c.FailSandboxes[sb.ID]
		startupDelay := c.StartupDelay
		c.mu.Unlock()

		// Apply startup delay if configured
		if startupDelay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(startupDelay):
			}
		}

		// Transition the sandbox
		if shouldFail {
			c.transitionToDead(ctx, sb.ID)
		} else {
			c.transitionToRunning(ctx, &sb)
		}
	}
}

func (c *MockSandboxController) transitionToRunning(ctx context.Context, sb *compute_v1alpha.Sandbox) {
	c.Log.Debug("transitioning sandbox to RUNNING", "id", sb.ID)

	// Build patch attributes
	patchSb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
	}

	attrs := []any{
		entity.Ref(entity.DBId, sb.ID),
		patchSb.Encode,
	}

	// Add network if configured
	if c.AssignNetwork {
		network := compute_v1alpha.Network{
			Address: "10.0.0.100/24",
			Subnet:  "mock-bridge",
		}
		attrs = append(attrs, entity.Component(compute_v1alpha.SandboxNetworkId, network.Encode()))
	}

	// Add schedule info for the mock node
	schedule := &compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{
			Node: c.NodeID,
		},
	}
	attrs = append(attrs, schedule.Encode)

	_, err := c.EAC.Patch(ctx, entity.New(attrs...).Attrs(), 0)
	if err != nil {
		c.Log.Error("failed to transition sandbox to RUNNING", "id", sb.ID, "error", err)
		return
	}

	c.Log.Info("sandbox is now RUNNING", "id", sb.ID)

	if c.OnSandboxReady != nil {
		c.OnSandboxReady(sb.ID)
	}
}

func (c *MockSandboxController) transitionToDead(ctx context.Context, id entity.Id) {
	c.Log.Debug("transitioning sandbox to DEAD", "id", id)

	patchSb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.DEAD,
	}

	_, err := c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		patchSb.Encode,
	).Attrs(), 0)
	if err != nil {
		c.Log.Error("failed to transition sandbox to DEAD", "id", id, "error", err)
		return
	}

	c.Log.Info("sandbox is now DEAD", "id", id)

	if c.OnSandboxFailed != nil {
		c.OnSandboxFailed(id)
	}
}

// WaitForSandbox waits for a sandbox to reach a terminal state (RUNNING or DEAD).
// Returns the final status or an error if the context times out.
func (c *MockSandboxController) WaitForSandbox(ctx context.Context, id entity.Id) (compute_v1alpha.SandboxStatus, error) {
	pollInterval := c.PollInterval
	if pollInterval == 0 {
		pollInterval = 50 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			resp, err := c.EAC.Get(ctx, id.String())
			if err != nil {
				continue
			}

			var sb compute_v1alpha.Sandbox
			sb.Decode(resp.Entity().Entity())

			if sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.DEAD {
				return sb.Status, nil
			}
		}
	}
}

// CreateTestSandbox creates a sandbox entity for testing with minimal configuration.
func CreateTestSandbox(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
	name string,
	spec *compute_v1alpha.SandboxSpec,
) (entity.Id, error) {
	sbID := entity.Id("sandbox/" + name)

	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec:   *spec,
	}

	_, err := eac.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name:   name,
			Labels: types.LabelSet("test", "true"),
		}).Encode,
		entity.DBId, sbID,
		sb.Encode,
	).Attrs())
	if err != nil {
		return "", err
	}

	return sbID, nil
}

// CreateMinimalSandboxSpec creates a minimal sandbox spec for testing.
func CreateMinimalSandboxSpec(image string) *compute_v1alpha.SandboxSpec {
	return &compute_v1alpha.SandboxSpec{
		Container: []compute_v1alpha.SandboxSpecContainer{
			{
				Name:  "app",
				Image: image,
			},
		},
	}
}
