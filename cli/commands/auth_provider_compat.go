package commands

import (
	"context"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/identityproviders"
)

// Older coordinators expose providers only through EntityAccess. Reuse the
// provider implementation locally so legacy reads receive the same credential
// filtering and writes receive the same validation as the preferred RPC.
func providerRPCClient(ctx *Context) (rpc.Client, error) {
	client, err := ctx.RPCClient("dev.miren.runtime/identity-providers")
	if err != nil {
		if !isMissingRPC(err) {
			return nil, err
		}
		return legacyProviderRPCClient(ctx)
	}
	return &providerCompatClient{NetworkClient: client, ctx: ctx}, nil
}

func legacyProviderRPCClient(ctx *Context) (rpc.Client, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	return &legacyProviderClient{Client: rpc.LocalClient(ingress_v1alpha.AdaptIdentityProviders(
		identityproviders.NewServer(ingress.NewClient(ctx.Log, client))))}, nil
}

type providerCompatClient struct {
	*rpc.NetworkClient
	ctx *Context
}

func (c *providerCompatClient) Call(ctx context.Context, method string, args, result any) error {
	err := requireRPCMethod(ctx, c.NetworkClient, method)
	if err == nil {
		err = c.NetworkClient.Call(ctx, method, args, result)
	}
	if !isMissingRPC(err) {
		return err
	}
	legacy, err := legacyProviderRPCClient(c.ctx)
	if err != nil {
		return err
	}
	return legacy.Call(ctx, method, args, result)
}

type legacyProviderClient struct{ rpc.Client }

func (c *legacyProviderClient) Call(ctx context.Context, method string, args, result any) error {
	// The legacy path follows the old coordinator's EntityAccess policy, not
	// the newer RPC's operator-only restriction, just as the old CLI did. This
	// identity bypasses only the in-process gate; remote EntityAccess still
	// authenticates and authorizes the actual caller with its real credentials.
	ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodJWT, Subject: "legacy-cli"})
	return c.Client.Call(ctx, method, args, result)
}
