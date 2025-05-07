package rpc

import (
	"context"
	"crypto/rand"
	"io"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"miren.dev/runtime/pkg/cond"
)

type ActorResolver interface {
	Resolve(ctx context.Context, name string) (string, error)
}

type ResolverFunc func(ctx context.Context, name string) (string, error)

func (f ResolverFunc) Resolve(ctx context.Context, name string) (string, error) {
	return f(ctx, name)
}

type ActorRegistry interface {
	Register(ctx context.Context, name string, iface *Interface) error
	Client(ctx context.Context, name string) (Client, error)
	Close(ctx context.Context) error
}

type LocalActorRegistry struct {
	mu sync.Mutex

	objects map[OID]*heldCapability
}

func NewLocalActorRegistry() ActorRegistry {
	return &LocalActorRegistry{
		objects: make(map[OID]*heldCapability),
	}
}

func (r *LocalActorRegistry) Close(ctx context.Context) error {
	return nil
}

func (r *LocalActorRegistry) Register(ctx context.Context, name string, iface *Interface) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	oid := OID(base58.Encode([]byte(name)))

	if _, ok := r.objects[oid]; ok {
		panic("actor already registered")
	}

	r.objects[oid] = &heldCapability{
		heldInterface: &heldInterface{
			Interface: iface,
		},
	}

	return nil
}

func (r *LocalActorRegistry) Client(ctx context.Context, name string) (Client, error) {
	oid := OID(base58.Encode([]byte(name)))

	return &ActorClient{
		oid: oid,
		reg: r,
	}, nil
}

func (a *LocalActorRegistry) newCapability(i *Interface) *Capability {
	buf := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		panic(err)
	}

	oid := OID(base58.Encode(buf))

	capa := &Capability{
		OID: oid,
	}

	hc := &heldCapability{
		heldInterface: &heldInterface{
			Interface: i,
		},
		lastContact: time.Now(),
	}

	if i.restoreState != nil {
		if rs, err := i.restoreState.RestoreState(i); err == nil {
			capa.RestoreState = &InterfaceState{
				Interface: i.name,
				Data:      rs,
			}
		}
	} else if !i.forbidRestore {
		capa.RestoreState = &InterfaceState{
			Interface: i.name,
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	hc.refs.Add(1)

	a.objects[oid] = hc

	return capa
}

type ActorCall struct {
	reg *LocalActorRegistry

	argData []byte
	results any
}

var _ Call = (*ActorCall)(nil)

func (a *ActorCall) NewCapability(i *Interface) *Capability {
	return a.reg.newCapability(i)
}

func (a *ActorCall) Args(v any) {
	cbor.Unmarshal(a.argData, v)
}

func (a *ActorCall) Results(v any) {
	a.results = v
}

func (a *ActorCall) NewClient(capa *Capability) Client {
	return nil
}

type ActorClient struct {
	name string
	oid  OID
	reg  *LocalActorRegistry
}

var _ Client = (*ActorClient)(nil)

func (a *ActorClient) Call(ctx context.Context, name string, arg, ret any) error {
	hc, ok := a.reg.objects[a.oid]
	if !ok {
		return cond.NotFound("object", a.oid)
	}

	iface := hc.Interface

	m, ok := iface.methods[name]
	if !ok {
		return cond.NotFound("method", name)
	}

	data, err := cbor.Marshal(arg)
	if err != nil {
		return err
	}

	call := &ActorCall{
		reg:     a.reg,
		argData: data,
	}

	err = m.Handler(ctx, call)
	if err != nil {
		return err
	}

	rdata, err := cbor.Marshal(call.results)
	if err != nil {
		return err
	}

	return cbor.Unmarshal(rdata, ret)
}

func (a *ActorClient) CallWithCaps(ctx context.Context, method string, args, result any, caps map[OID]*InlineCapability) error {
	return a.Call(ctx, method, args, result)
}

func (a *ActorClient) NewInlineCapability(i *Interface, lower any) (*InlineCapability, OID, *Capability) {
	return nil, "", nil
}

func (a *ActorClient) Close() error {
	return nil
}

func (a *ActorClient) NewClient(capa *Capability) Client {
	return &ActorClient{
		oid: capa.OID,
		reg: a.reg,
	}
}
