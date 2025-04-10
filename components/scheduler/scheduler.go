package scheduler

import (
	"context"
	"log/slog"
	"sync"

	"github.com/davecgh/go-spew/spew"
	compute "miren.dev/runtime/api/compute/v1alpha"
	eas "miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/stream"
)

type sandbox struct {
	compute.Sandbox
	*entity.Entity
}

type Scheduler struct {
	log   *slog.Logger
	nodes map[entity.Id]*compute.Node

	assigning sync.Mutex
}

func (s *Scheduler) gatherSandboxes(ctx context.Context, eac *eas.EntityAccessClient) ([]*sandbox, error) {
	results, err := eac.List(ctx, entity.Keyword(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return nil, err
	}

	entities := results.Values()

	var ret []*sandbox

	for _, ent := range entities {
		var sandbox sandbox
		sandbox.Entity = ent.Entity()

		sandbox.Decode(sandbox.Entity)
		ret = append(ret, &sandbox)
	}

	return ret, nil
}

func (s *Scheduler) gatherNodes(ctx context.Context, eac *eas.EntityAccessClient) ([]*compute.Node, error) {
	results, err := eac.List(ctx, entity.Keyword(entity.EntityKind, compute.KindNode))
	if err != nil {
		return nil, err
	}

	entities := results.Values()

	var ret []*compute.Node

	for _, ent := range entities {
		var node compute.Node

		node.Decode(ent.Entity())
		ret = append(ret, &node)
	}

	s.log.Debug("gathered nodes", "count", len(ret))

	return ret, nil
}

func NewScheduler(ctx context.Context, log *slog.Logger, eac *eas.EntityAccessClient) (*Scheduler, error) {
	s := &Scheduler{
		log:   log,
		nodes: make(map[entity.Id]*compute.Node),
	}

	nodes, err := s.gatherNodes(ctx, eac)
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		s.nodes[node.ID] = node
	}

	return s, nil
}

func (s *Scheduler) FindNodeById(id entity.Id) (*compute.Node, error) {
	node, ok := s.nodes[id]
	if !ok {
		return nil, cond.NotFound(id)
	}

	return node, nil
}

func (s *Scheduler) Watch(ctx context.Context, eac *eas.EntityAccessClient) error {
	index := entity.Keyword(entity.EntityKind, compute.KindSandbox)

	_, err := eac.WatchIndex(ctx, index, stream.Callback(func(op *eas.EntityOp) error {

		if op == nil {
			return nil
		}

		switch op.Operation() {
		case 1, 2:
			// fine
		default:
			return nil
		}

		err := s.assignSandbox(ctx, op.Entity().Entity(), eac)
		if err != nil {
			s.log.Error("failed to assign sandbox", "error", err, "sandbox", op.Entity().Id())
		}

		return nil
	}))
	return err
}

func (s *Scheduler) AssignSandboxes(ctx context.Context, eac *eas.EntityAccessClient) error {
	// Get all sandboxes
	sandboxes, err := s.gatherSandboxes(ctx, eac)
	if err != nil {
		return err
	}

	// Find first available node
	var firstNode *compute.Node
	for _, node := range s.nodes {
		firstNode = node
		break
	}

	if firstNode == nil {
		return cond.NotFound("no nodes available for scheduling")
	}

	s.log.Debug("considering sandboxes for assignment", "count", len(sandboxes))

	// Look for unscheduled sandboxes
	for _, sandbox := range sandboxes {
		err = s.assignSandbox(ctx, sandbox.Entity, eac)
		if err != nil {
			s.log.Error("failed to assign sandbox", "error", err, "sandbox", sandbox.Entity.ID)
		}
	}

	return nil
}

func (s *Scheduler) assignSandbox(ctx context.Context, ent *entity.Entity, eac *eas.EntityAccessClient) error {
	var sandbox sandbox
	sandbox.Entity = ent
	sandbox.Decode(sandbox.Entity)

	// Skip if already scheduled
	if _, ok := sandbox.Get(compute.ScheduleKeyId); ok {
		return nil
	}

	s.assigning.Lock()
	defer s.assigning.Unlock()

	// TODO here is where a real scheduling algorithm will go.
	// Find first available node
	var firstNode *compute.Node
	for _, node := range s.nodes {
		firstNode = node
		break
	}

	if firstNode == nil {
		s.log.Error("no nodes available for scheduling")
		return nil
	}

	s.log.Error("sandbox attrs")
	spew.Dump(sandbox.Entity.Attrs)

	se := compute.Schedule{
		Key: compute.Key{
			Kind: compute.KindSandbox,
			Node: firstNode.ID,
		},
	}

	err := sandbox.Entity.Update(se.Encode())
	if err != nil {
		s.log.Error("failed to update sandbox entity", "error", err)
		return err
	}

	// Create scheduler index attribute
	//schedIndex := sch.Index(sb.KindSandbox, string(firstNode.ID))
	//sandbox.Entity.Attrs = append(sandbox.Entity.Attrs, schedIndex)

	s.log.Error("post sandbox attrs")
	spew.Dump(sandbox.Attrs)

	var rpcE eas.Entity
	rpcE.SetId(string(sandbox.Entity.ID))
	rpcE.SetAttrs(sandbox.Attrs)

	if _, err := eac.Put(ctx, &rpcE); err != nil {
		s.log.Error("failed to assign sandbox", "error", err, "sandbox", sandbox.Entity.ID)
		return err
	}

	return nil
}
