package etcdreg

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
)

type ActorRegistry struct {
	log *slog.Logger
	rs  *rpc.State
	ec  *clientv3.Client

	mu      sync.Mutex
	closing bool
	running map[string]bool
}

var _ rpc.ActorRegistry = (*ActorRegistry)(nil)

func NewActorRegistry(log *slog.Logger, rs *rpc.State, ec *clientv3.Client) rpc.ActorRegistry {
	return &ActorRegistry{
		log:     log,
		rs:      rs,
		ec:      ec,
		running: make(map[string]bool),
	}
}

type ActorStater interface {
	ActorState() any
}

func (r *ActorRegistry) setActorState(ctx context.Context, name string, as ActorStater) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.running[name] = false

	sv := as.ActorState()

	data, err := cbor.Marshal(sv)
	if err != nil {
		r.log.Error("failed to marshal actor state", "name", name, "err", err)
		return
	}

	_, err = r.ec.Put(ctx, "/actor/state/"+name, string(data))
	if err != nil {
		r.log.Error("failed to put actor state", "name", name, "err", err)
		return
	}
}

func (r *ActorRegistry) getActorState(ctx context.Context, name string) ([]byte, error) {
	resp, err := r.ec.Get(ctx, "/actor/state/"+name, clientv3.WithLimit(1))
	if err != nil {
		return nil, err
	}

	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	return resp.Kvs[0].Value, nil
}

func (r *ActorRegistry) restoreActorState(ctx context.Context, name string, as ActorStater) context.Context {
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

func (r *ActorRegistry) acquirePath(ctx context.Context, name string, id clientv3.LeaseID) bool {
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
		gr, err := r.ec.Get(ctx, path)
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

func (r *ActorRegistry) Register(ctx context.Context, name string, iface *rpc.Interface) error {
	lr, err := r.ec.Grant(ctx, 60)
	if err != nil {
		r.log.Error("failed to grant lease", "name", name, "err", err)
		return err
	}

	owned := r.acquirePath(ctx, name, lr.ID)

	go func() {
		//nolint:errcheck
		defer r.ec.Revoke(ctx, lr.ID)

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
				_, err := r.ec.KeepAliveOnce(ctx, lr.ID)
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

func (r *ActorRegistry) Close(ctx context.Context) error {
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

func (r *ActorRegistry) Client(ctx context.Context, name string) (rpc.Client, error) {
	path := "/actor/registry/" + name
	resp, err := r.ec.Get(ctx, path)
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
