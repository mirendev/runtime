package commands

import "miren.dev/runtime/api/app/app_v1alpha"

func Apps(ctx *Context, opts struct {
	ConfigCentric
}) error {
	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	crud := app_v1alpha.NewCrudClient(crudcl)

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
