package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/api/app/app_v1alpha"
)

type AppDeleteOptions struct {
	Yes     bool   `short:"y" long:"yes" description:"Skip confirmation prompt"`
	Force   bool   `short:"f" long:"force" description:"Deprecated: use --yes to skip confirmation"`
	AppName string `position:"0" usage:"Deprecated: use --app instead"`
	AppCentric
}

func (o *AppDeleteOptions) Validate(flags *GlobalFlags) error {
	if o.App == "" && o.AppName != "" {
		o.App = o.AppName
	}
	return o.AppCentric.Validate(flags)
}

func AppDelete(ctx *Context, opts AppDeleteOptions) error {
	if opts.AppName != "" {
		fmt.Fprintln(os.Stderr, "Warning: the positional app in 'miren app delete' is deprecated; use --app instead.")
	}
	appName := opts.App
	if appName == "" {
		appName = opts.AppName
	}
	if appName == "" {
		return fmt.Errorf("app is required")
	}

	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	crud := app_v1alpha.NewCrudClient(crudcl)

	confirmed, err := confirmDeletion(fmt.Sprintf("Delete app '%s' and all its versions, artifacts, pools, and routes?", appName), opts.Yes, opts.Force, false)
	if err != nil {
		return err
	}
	if !confirmed {
		ctx.Printf("Deletion cancelled\n")
		return nil
	}

	_, err = crud.Destroy(ctx, appName)
	if err != nil {
		return err
	}

	ctx.Printf("Deleted app %s\n", appName)
	return nil
}
