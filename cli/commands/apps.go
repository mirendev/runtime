package commands

import "miren.dev/runtime/app"

func Apps(ctx *Context, opts struct {
	ConfigCentric
}) error {
	crudcl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	crud := app.CrudClient{Client: crudcl}

	apps, err := crud.List(ctx)
	if err != nil {
		return err
	}

	if len(apps.Apps()) == 0 {
		ctx.Printf("No apps found\n")
		return nil
	}

	for _, a := range apps.Apps() {
		ctx.Info("%s", a.Name())
	}

	return nil
}
