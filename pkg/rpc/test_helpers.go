package rpc

import (
	"context"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/idgen"
)

type localEnv struct {
	mu   sync.RWMutex
	caps map[OID]*Interface
}

type localCall struct {
	*localEnv
	args any
	ret  any
}

func (l *localCall) NewCapability(i *Interface) *Capability {
	oid := OID(idgen.Gen("lcap"))
	l.mu.Lock()
	l.caps[oid] = i
	l.mu.Unlock()

	return &Capability{
		OID: oid,
	}
}

func (l *localClient) NewCapability(i *Interface) *Capability {
	oid := OID(idgen.Gen("lcap"))
	l.mu.Lock()
	l.caps[oid] = i
	l.mu.Unlock()

	return &Capability{
		OID: oid,
	}
}

func (l *localCall) NewClient(capa *Capability) *NetworkClient {
	l.mu.RLock()
	iface := l.caps[capa.OID]
	l.mu.RUnlock()

	return &NetworkClient{
		oid: capa.OID,

		localClient: &localClient{
			localEnv: l.localEnv,
			iface:    iface,
		},
	}
}

func (l *localClient) NewClient(capa *Capability) *NetworkClient {
	l.mu.RLock()
	iface := l.caps[capa.OID]
	l.mu.RUnlock()

	return &NetworkClient{
		oid: capa.OID,

		localClient: &localClient{
			localEnv: l.localEnv,
			iface:    iface,
		},
	}
}

type localClient struct {
	*localEnv
	iface *Interface
}

func (l *localClient) Call(ctx context.Context, name string, arg, ret any) error {
	m, ok := l.iface.methods[name]
	if !ok {
		panic("method not found")
	}

	data, err := cbor.Marshal(arg)
	if err != nil {
		return err
	}

	call := &NetworkCall{
		argData: data,
		local: &localCall{
			localEnv: l.localEnv,
		},
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

func LocalClient(iface *Interface) *NetworkClient {
	return &NetworkClient{
		localClient: &localClient{
			localEnv: &localEnv{
				caps: make(map[OID]*Interface),
			},
			iface: iface,
		},
	}
}

func Local(args ...any) *NetworkCall {
	md := map[string]any{}

	for i := 0; i < len(args); i += 2 {
		md[args[i].(string)] = args[i+1]
	}

	data, err := cbor.Marshal(md)
	if err != nil {
		panic(err)
	}

	return &NetworkCall{
		argData: data,
		local: &localCall{
			localEnv: &localEnv{
				caps: make(map[OID]*Interface),
			},
		},
	}
}
