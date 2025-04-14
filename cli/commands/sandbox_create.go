package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

func SandboxCreate(ctx *Context, opts struct {
	Address string `short:"a" long:"address" description:"Address to listen on" default:":8443"`
}) error {
	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		return fmt.Errorf("failed to create RPC server: %w", err)
	}

	client, err := rs.Connect(opts.Address, "entities")
	if err != nil {
		return fmt.Errorf("failed to connect to RPC server: %w", err)
	}

	eac := entityserver_v1alpha.EntityAccessClient{Client: client}

	id := fmt.Sprintf("sandbox/test-%d", time.Now().Unix())

	// Test creating a sandbox entity
	sandbox := &entityserver_v1alpha.Entity{}
	sandbox.SetId(id)
	sandbox.SetAttrs([]entity.Attr{
		entity.Keyword(entity.Ident, id),
	})

	_, err = eac.Put(ctx, sandbox)
	return err
}
