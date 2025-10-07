package actors

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/api/actor/actor_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

type Registry struct {
	log *slog.Logger
	rs  *rpc.State
	tc  *entityserver.Client
	ec  *entityserver.Client
	es  *entityserver.Session

	id       string
	entityId entity.Id

	mu      sync.Mutex
	closing bool
	running map[string]bool
}

var _ rpc.ActorRegistry = (*Registry)(nil)

func NewRegistry(log *slog.Logger, rs *rpc.State, id string, ec *entityserver.Client) rpc.ActorRegistry {
	return &Registry{
		log:     log,
		rs:      rs,
		id:      id,
		tc:      ec,
		running: make(map[string]bool),
	}
}

func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	es, ec, err := r.tc.NewSession(ctx, r.id)
	if err != nil {
		return err
	}

	r.es = es
	r.ec = ec

	var an actor_v1alpha.Node
	an.Endpoint = append(an.Endpoint, r.rs.ListenAddr())

	pr, err := r.ec.Create(ctx, r.id, &an)
	if err != nil {
		return err
	}

	r.entityId = pr
	return err
}

type ActorStater interface {
	ActorState() any
}

func (r *Registry) setActorState(ctx context.Context, name string, as ActorStater) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.running[name] = false

	sv := as.ActorState()

	data, err := cbor.Marshal(sv)
	if err != nil {
		r.log.Error("failed to marshal actor state", "name", name, "err", err)
		return
	}

	var an actor_v1alpha.Actor

	err = r.ec.Get(ctx, name, &an)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			an.State = data
			_, err = r.ec.Create(ctx, name, &an)
			if err != nil {
				r.log.Error("failed to create actor", "name", name, "err", err)
			}
		} else {
			r.log.Error("failed to get actor", "name", name, "err", err)
			return
		}
	} else {
		an.State = data
		err = r.ec.Update(ctx, &an)
		if err != nil {
			r.log.Error("failed to update actor", "name", name, "err", err)
		}
	}
}

func (r *Registry) getActorState(ctx context.Context, name string) ([]byte, error) {
	var an actor_v1alpha.Actor

	err := r.ec.Get(ctx, name, &an)
	if err != nil {
		return nil, err
	}

	return an.State, nil
}

func (r *Registry) restoreActorState(ctx context.Context, name string, as ActorStater) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()

	ok := r.running[name]
	if ok {
		return ctx
	}

	state, err := r.getActorState(ctx, name)
	if err != nil {
		r.log.Error("failed to get actor state", "name", name, "err", err)
		return ctx
	}

	if state != nil {
		sv := as.ActorState()

		err = cbor.Unmarshal(state, sv)
		if err != nil {
			r.log.Error("failed to unmarshal actor state", "name", name, "err", err)
		}
	}

	return ctx
}

func (r *Registry) Register(ctx context.Context, name string, iface *rpc.Interface) error {
	var act actor_v1alpha.Actor

	owned := false

	err := r.ec.Get(ctx, name, &act)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			act.Node = r.entityId

			err = r.ec.Update(ctx, &act)
			if err == nil {
				owned = true
			}
		}
	} else if act.Node == r.entityId {
		owned = true
	}

	ch := r.ec.WatchEntity(ctx, r.entityId)

	go func() {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		owned := owned

		ticker := time.NewTicker(30 * time.Second)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			// ok
			case wr, ok := <-ch:
				if !ok {
					r.log.Info("actor deleted", "name", name)
					return
				}

				var act actor_v1alpha.Actor
				act.Decode(wr)

				switch act.Node {
				case "":
					r.log.Info("actor not owned by any node", "name", name)
				case r.entityId:
					// we still own it!
					owned = true
				default:
					r.log.Info("actor not owned by this node", "name", name, "node", act.Node)
				}

				r.mu.Lock()
				closing := r.closing
				r.mu.Unlock()

				if closing {
					return
				}
			}

			if !owned {
				err = r.ec.UpdateAttrs(ctx, act.ID, entity.Ref(actor_v1alpha.ActorNodeId, r.entityId))
				if err != nil {
					r.log.Error("failed to update actor", "name", name, "err", err)
				} else {
					owned = true
					r.log.Info("actor updated to this node", "name", name)
				}
			}
		}
	}()

	if sv, ok := iface.Value().(ActorStater); ok {
		iface.SetAroundContext(func(ctx context.Context, call rpc.Call) (context.Context, func()) {
			ctx = r.restoreActorState(ctx, name, sv)

			return ctx, func() {
				r.setActorState(ctx, name, sv)
			}
		})
	}

	r.log.Info("actor registered", "name", name, "ep", r.rs.ListenAddr())

	r.rs.Server().ExposeValue(name, iface)
	return nil
}

func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closing = true

	r.es.Close()

	return nil
}

func (r *Registry) Client(ctx context.Context, name string) (rpc.Client, error) {
	var act actor_v1alpha.Actor

	err := r.ec.Get(ctx, name, &act)
	if err != nil {
		return nil, cond.NotFound("actor", name)
	}

	if act.Node == "" {
		return nil, cond.NotFound("actor", name)
	}

	var node actor_v1alpha.Node
	err = r.ec.GetById(ctx, act.Node, &node)
	if err != nil {
		return nil, cond.NotFound("node", act.Node)
	}

	c, err := r.rs.Connect(node.Endpoint[0], name)
	if err != nil {
		return nil, err
	}

	return c, nil
}
