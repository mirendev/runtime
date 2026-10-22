package commands

import (
	"context"
	"errors"
	"slices"

	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
)

// Only an absent capability or method permits downgrading to EntityAccess.
// In particular, an authorization failure or missing resource must not do so.
func isMissingRPC(err error) bool {
	if resolve, ok := errors.AsType[*rpc.ResolveError](err); ok && resolve.Kind == rpc.ResolveLookupError {
		// Lookup replies also carry malformed-key and restore failures. Only
		// this response proves that the named service is absent.
		return resolve.Msg == "unknown object: "+resolve.Name
	}
	missing, ok := errors.AsType[cond.ErrNotFound](err)
	return ok && missing.Category == "method"
}

// Older HTTP RPC servers return an untyped 404 for absent methods. Successful
// introspection proves absence without confusing it with a resource 404.
func requireRPCMethod(ctx context.Context, client *rpc.NetworkClient, method string) error {
	methods, err := client.ListMethods(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(methods, method) {
		return cond.NotFound("method", method)
	}
	return nil
}
