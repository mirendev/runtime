package rpc

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"miren.dev/runtime/pkg/cond"
)

type Dispatcher interface {
	Dispatch(ctx context.Context, oid OID, method string, call Call) error
	Bind(oid OID, iface *Interface) error
}

type localRegistry struct {
	mu      sync.Mutex
	objects map[OID]*heldCapability
}

func (r *localRegistry) lookupMethod(oid OID, method string) (*Method, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	iface, ok := r.objects[oid]
	if !ok {
		return nil, cond.NotFound("object", oid)
	}

	mm, ok := iface.methods[method]
	if !ok {
		return nil, cond.NotFound("method", method)
	}

	return &mm, nil
}

func (r *localRegistry) Dispatch(ctx context.Context, oid OID, method string, call Call) error {
	mm, err := r.lookupMethod(oid, method)
	if err != nil {
		return err
	}

	tracer := Tracer()

	ctx, span := tracer.Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)

	defer span.End()

	span.SetAttributes(attribute.String("oid", string(oid)))

	return mm.Handler(ctx, call)
}
