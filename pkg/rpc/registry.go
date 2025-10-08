package rpc

import (
	"context"
)

type Dispatcher interface {
	Dispatch(ctx context.Context, oid OID, method string, call Call) error
	Bind(oid OID, iface *Interface) error
}

