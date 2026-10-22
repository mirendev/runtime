package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
)

func RouteUnsetDefault(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cl, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}
	ingressClient := ingress_v1alpha.NewRoutesClient(cl)

	oldDefault, err := ingressClient.UnsetDefault(ctx)
	if err != nil {
		return fmt.Errorf("failed to unset default route: %w", err)
	}

	if oldDefault.Changed() {
		ctx.Printf("Removed default route from app: %s\n", oldDefault.App())
	} else {
		ctx.Printf("No default route was set\n")
	}

	return nil
}
