package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
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

	cl, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}
	routes := ingress_v1alpha.NewRoutesClient(cl)
	_, err = routes.SetDefault(ctx, appName)
	if err != nil {
		return fmt.Errorf("failed to set default route: %w", err)
	}

	ctx.Printf("Set default route to: %s\n", appName)
	return nil
}
