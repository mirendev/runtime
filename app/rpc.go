package app

import "context"

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg app -input rpc.yml -output rpc.gen.go

type RPCCrud struct {
	Access *AppAccess
}

var _ Crud = &RPCCrud{}

func (r *RPCCrud) New(ctx context.Context, state *CrudNew) error {
	name := state.Args().Name()
	_, err := r.Access.LoadApp(ctx, name)
	if err == nil {
		// ok, return the current one.
		// TODO this is a bad id.
		state.Results().SetId(name)
		return nil
	}

	err = r.Access.CreateApp(ctx, &AppConfig{
		Name: name,
	})
	if err != nil {
		return err
	}

	// TODO this is a bad id.
	state.Results().SetId(name)

	return nil
}
