package scheduler

import (
	"context"
	"log/slog"
	"math/rand"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// Controller assigns sandboxes to nodes for execution.
// It watches sandbox entities and adds a ScheduleKey attribute to assign
// each sandbox to an available node.
//
// Implements controller.ReconcileControllerI[*compute_v1alpha.Sandbox]
type Controller struct {
	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

// NewController creates a new scheduler controller
func NewController(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
) *Controller {
	return &Controller{
		log: log.With("module", "scheduler"),
		eac: eac,
	}
}

// Init initializes the controller.
// Required by ReconcileControllerI.
func (c *Controller) Init(ctx context.Context) error {
	c.log.Info("initializing scheduler controller")
	return nil
}

// Reconcile ensures the sandbox is assigned to a node.
// Called by the controller framework for both Add and Update events.
func (c *Controller) Reconcile(ctx context.Context, sandbox *compute_v1alpha.Sandbox, meta *entity.Meta) error {
	// Skip if already scheduled
	if _, ok := meta.Get(compute_v1alpha.ScheduleKeyId); ok {
		return nil
	}

	c.log.Debug("scheduling sandbox", "id", sandbox.ID)

	// Fetch fresh node data
	allNodes, err := c.gatherNodes(ctx)
	if err != nil {
		c.log.Error("failed to gather nodes", "error", err)
		return err
	}

	// Find available READY nodes
	var nodes []*compute_v1alpha.Node
	for _, node := range allNodes {
		if node.Status == compute_v1alpha.READY {
			nodes = append(nodes, node)
		}
	}

	if len(nodes) == 0 {
		c.log.Error("no nodes available for scheduling", "sandbox", sandbox.ID)
		return nil
	}

	// Pick a random ready node
	// TODO: implement smarter scheduling (load balancing, affinity, etc.)
	assignedNode := nodes[rand.Intn(len(nodes))]

	c.log.Info("assigning sandbox to node",
		"sandbox", sandbox.ID,
		"node", assignedNode.ID)

	// Add schedule key to the entity
	schedule := compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{
			Kind: compute_v1alpha.KindSandbox,
			Node: assignedNode.ID,
		},
	}

	if err := meta.Update(schedule.Encode()); err != nil {
		c.log.Error("failed to update sandbox with schedule", "error", err)
		return err
	}

	return nil
}

// gatherNodes fetches all node entities from the entity store
func (c *Controller) gatherNodes(ctx context.Context) ([]*compute_v1alpha.Node, error) {
	results, err := c.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		return nil, err
	}

	entities := results.Values()

	var ret []*compute_v1alpha.Node
	for _, ent := range entities {
		var node compute_v1alpha.Node
		node.Decode(ent.Entity())
		ret = append(ret, &node)
	}

	c.log.Debug("gathered nodes", "count", len(ret))
	return ret, nil
}
