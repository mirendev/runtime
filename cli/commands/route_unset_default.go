package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/ui"
)

func RouteUnsetDefault(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	ingressClient := ingress.NewClient(ctx.Log, cl)

	oldDefault, err := ingressClient.UnsetDefault(ctx)
	if err != nil {
		return fmt.Errorf("failed to unset default route: %w", err)
	}

	if oldDefault != nil {
		ctx.Printf("Removed default route from app: %s\n", ui.CleanEntityID(string(oldDefault.App)))
	} else {
		ctx.Printf("No default route was set\n")
	}

	return nil
}
