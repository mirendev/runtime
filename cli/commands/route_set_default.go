package commands

import (
	"fmt"

	apppkg "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/ingress"
)

func RouteSetDefault(ctx *Context, opts struct {
	AppName string `position:"0" usage:"Application name to set as default route"`
	ConfigCentric
}) error {
	appName := opts.AppName
	if appName == "" {
		if ac := loadLocalAppConfigOrWarn(); ac != nil && ac.Name != "" {
			appName = ac.Name
		}
	}
	if appName == "" {
		return fmt.Errorf("app is required")
	}

	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	appClient := apppkg.NewClient(ctx.Log, cl)
	ingressClient := ingress.NewClient(ctx.Log, cl)

	// Get the app to ensure it exists
	app, err := appClient.GetByName(ctx, appName)
	if err != nil {
		return fmt.Errorf("failed to get app %s: %w", appName, err)
	}

	ctx.Log.Info("setting default route", "app", app.ID)

	_, err = ingressClient.SetDefault(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to set default route: %w", err)
	}

	ctx.Printf("Set default route to: %s\n", appName)
	return nil
}
