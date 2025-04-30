package actors

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/actor/actor_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
)

type Registry struct {
	log *slog.Logger
	rs  *rpc.State
	ec  *entityserver.Client
	id  string

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
		ec:      ec,
		running: make(map[string]bool),
	}
}

func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var an actor_v1alpha.Node
	an.Endpoint = append(an.Endpoint, r.rs.ListenAddr())

	_, err := r.ec.Create(ctx, r.id, &an)
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

func (r *Registry) acquirePath(ctx context.Context, name string, id clientv3.LeaseID) (bool, error) {

	var an actor_v1alpha.Actor
	err := r.ec.Get(ctx, name, &an)
	if err != nil {
		return false, err
	}

	var al actor_v1alpha.ActorLease
	err = r.ec.Get(ctx, name+"/lease", &al)

	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return false, err
		}
	}

	path := "/actor/registry/" + name

	// Attempt to register the actor
	txn := r.ec.Txn(ctx).If(
		clientv3.Compare(clientv3.CreateRevision(path), "=", 0),
	).Then(
		clientv3.OpPut(path, r.rs.ListenAddr(), clientv3.WithLease(id)),
	).Else()

	tr, err := txn.Commit()
	if err != nil {
		r.log.Error("failed to commit transaction", "name", name, "err", err)
	}

	if !tr.Succeeded {
		gr, err := r.ec.KV.Get(ctx, path)
		if err != nil {
			r.log.Error("failed to get actor", "name", name, "err", err)
			return false
		}

		if len(gr.Kvs) == 0 {
			r.log.Error("actor not found", "name", name)
			return false
		}

		ep := string(gr.Kvs[0].Value)
		if ep != r.rs.ListenAddr() {
			r.log.Error("actor already registered", "name", name, "ep", ep)
		} else {
			r.log.Info("actor already owned by self", "name", name)
		}
		return false
	} else {
		r.log.Info("actor registered", "name", name, "ep", r.rs.ListenAddr())
		return true
	}
}

func (r *Registry) Register(ctx context.Context, name string, iface *rpc.Interface) error {
	lr, err := r.ec.Lease.Grant(ctx, 60)
	if err != nil {
		r.log.Error("failed to grant lease", "name", name, "err", err)
		return err
	}

	owned := r.acquirePath(ctx, name, lr.ID)

	go func() {
		defer r.ec.Lease.Revoke(ctx, lr.ID)

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		path := "/actor/registry/" + name
		wc := r.ec.Watch(ctx, path)

		owned := owned

		ticker := time.NewTicker(30 * time.Second)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, err := r.ec.Lease.KeepAliveOnce(ctx, lr.ID)
				if err != nil {
					r.log.Error("failed to keep lease alive", "name", name, "err", err)
					return
				}
			case wr := <-wc:
				if wr.Canceled {
					r.log.Error("watch canceled", "name", name, "err", wr.Err())
					wc = r.ec.Watch(ctx, path)
				}

				for _, ev := range wr.Events {
					switch ev.Type {
					case clientv3.EventTypeDelete:
						r.log.Info("actor lease deleted", "name", name)
					case clientv3.EventTypePut:
						r.log.Info("actor lease updated", "name", name)
						if ev.Kv == nil {
							r.log.Error("actor lease updated, but no value", "name", name)
						}
						if string(ev.Kv.Value) != r.rs.ListenAddr() {
							r.log.Error("actor lease updated, but not this node", "name", name, "ep", string(ev.Kv.Value))
						}
					}
				}

				r.mu.Lock()
				closing := r.closing
				r.mu.Unlock()

				if closing {
					return
				}

				if !owned {
					owned = r.acquirePath(ctx, name, lr.ID)
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

	for name, ok := range r.running {
		if ok {
			r.log.Info("actor still running", "name", name)
		} else {
			r.log.Info("actor not running", "name", name)
		}

		path := "/actor/registry/" + name
		txn := r.ec.Txn(ctx).If(
			clientv3.Compare(clientv3.Value(path), "=", r.rs.ListenAddr()),
		).Then(
			clientv3.OpDelete(path),
		)

		txn.Commit()
	}

	return nil
}

func (r *Registry) Client(ctx context.Context, name string) (rpc.Client, error) {
	path := "/actor/registry/" + name
	resp, err := r.ec.KV.Get(ctx, path)
	if err != nil {
		return nil, err
	}

	if len(resp.Kvs) == 0 {
		return nil, cond.NotFound("actor", name)
	}

	ep := string(resp.Kvs[0].Value)

	c, err := r.rs.Connect(ep, name)
	if err != nil {
		return nil, err
	}

	return c, nil
}
