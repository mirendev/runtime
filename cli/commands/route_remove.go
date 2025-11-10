package commands

import (
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/ui"
)

func RouteRemove(ctx *Context, opts struct {
	ConfigCentric
	Args struct {
		Host string `positional-arg-name:"host" description:"Hostname of the route to remove"`
	} `positional-args:"yes"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	host := opts.Args.Host

	// If no host provided, show interactive picker
	if host == "" {
		routes, err := ic.List(ctx)
		if err != nil {
			return err
		}

		if len(routes) == 0 {
			ctx.Printf("No routes found\n")
			return nil
		}

		// Create picker items
		items := make([]ui.PickerItem, len(routes))
		for i, r := range routes {
			displayHost := r.Route.Host
			if displayHost == "" && r.Route.Default {
				displayHost = "(default)"
			}
			appDisplay := ui.CleanEntityID(string(r.Route.App))

			items[i] = ui.TablePickerItem{
				Columns: []string{displayHost, appDisplay},
				ItemID:  r.Route.Host,
			}
		}

		// Run the picker
		selected, err := ui.RunPicker(items,
			ui.WithTitle("Select a route to remove:"),
			ui.WithHeaders([]string{"HOST", "APP"}),
		)

		if err != nil {
			// If we can't run interactive mode (no TTY), show available routes
			ctx.Printf("Cannot run interactive mode. Available routes:\n")
			for _, r := range routes {
				displayHost := r.Route.Host
				if displayHost == "" && r.Route.Default {
					displayHost = "(default)"
				}
				ctx.Printf("  %s → %s\n", displayHost, ui.CleanEntityID(string(r.Route.App)))
			}
			ctx.Printf("\nUsage: miren route remove <host>\n")
			return nil
		}

		if selected == nil {
			// User cancelled
			return nil
		}

		host = selected.ID()
	}

	// Delete the route
	err = ic.DeleteByHost(ctx, host)
	if err != nil {
		return err
	}

	ctx.Printf("Route removed: %s\n", host)
	return nil
}
