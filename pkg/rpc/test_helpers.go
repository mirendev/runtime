package rpc

import (
	"context"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/idgen"
)

type localEnv struct {
	caps map[OID]*Interface
}

type localCall struct {
	*localEnv
	args any
	ret  any
}

func (l *localCall) NewCapability(i *Interface) *Capability {
	oid := OID(idgen.Gen("lcap"))
	l.caps[oid] = i

	return &Capability{
		OID: oid,
	}
}

func (l *localCall) NewClient(capa *Capability) *Client {
	return &Client{
		oid: capa.OID,

		localClient: &localClient{
			localEnv: l.localEnv,
			iface:    l.caps[capa.OID],
		},
	}
}

func (l *localClient) NewClient(capa *Capability) *Client {
	return &Client{
		oid: capa.OID,

		localClient: &localClient{
			localEnv: l.localEnv,
			iface:    l.caps[capa.OID],
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

	call := &Call{
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

func LocalClient(iface *Interface) *Client {
	return &Client{
		localClient: &localClient{
			localEnv: &localEnv{
				caps: make(map[OID]*Interface),
			},
			iface: iface,
		},
	}
}

func Local(args ...any) *Call {
	md := map[string]any{}

	for i := 0; i < len(args); i += 2 {
		md[args[i].(string)] = args[i+1]
	}

	data, err := cbor.Marshal(md)
	if err != nil {
		panic(err)
	}

	return &Call{
		argData: data,
		local: &localCall{
			localEnv: &localEnv{
				caps: make(map[OID]*Interface),
			},
		},
	}
}
