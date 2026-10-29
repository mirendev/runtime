package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func RouteRemove(ctx *Context, opts struct {
	Host string `position:"0" usage:"Hostname of the route to remove"`
	Yes  bool   `short:"y" long:"yes" description:"Skip confirmation prompt"`
	ConfigCentric
}) error {
	if opts.Host == "" && !ui.IsInteractive() {
		return fmt.Errorf("missing route argument; use 'miren route delete <route>'")
	}
	client, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}

	ic := ingress_v1alpha.NewRoutesClient(client)

	host := opts.Host

	// If no host provided, show interactive picker
	if host == "" {
		res, err := ic.List(ctx, "")
		if err != nil {
			return err
		}

		routes := res.Routes()
		if len(routes) == 0 {
			ctx.Printf("No routes found\n")
			return nil
		}

		// Create picker items
		items := make([]ui.PickerItem, len(routes))
		for i, r := range routes {
			displayHost := r.Host()
			if displayHost == "" && r.DefaultRoute() {
				displayHost = "(default)"
			}
			appDisplay := r.App()

			items[i] = ui.TablePickerItem{
				Columns: []string{displayHost, appDisplay},
				ItemID:  r.Host(),
			}
		}

		// Run the picker
		selected, err := ui.RunPicker(items,
			ui.WithTitle("Select a route to remove:"),
			ui.WithHeaders([]string{"ROUTE", "APP"}),
		)

		if err != nil {
			// If we can't run interactive mode (no TTY), show available routes
			ctx.Printf("Cannot run interactive mode. Available routes:\n")
			for _, r := range routes {
				displayHost := r.Host()
				if displayHost == "" && r.DefaultRoute() {
					displayHost = "(default)"
				}
				ctx.Printf("  %s → %s\n", displayHost, r.App())
			}
			ctx.Printf("\nUsage: miren route delete <route>\n")
			return nil
		}

		if selected == nil {
			// User cancelled
			return nil
		}

		host = selected.ID()
	}

	displayHost := host
	if displayHost == "" {
		displayHost = "(default)"
	}
	confirmed, err := confirmDeletion(fmt.Sprintf("Delete route %s?", displayHost), opts.Yes, false, true)
	if err != nil {
		return err
	}
	if !confirmed {
		ctx.Printf("Deletion cancelled\n")
		return nil
	}

	// Delete the route
	_, err = ic.Remove(ctx, host)
	if err != nil {
		return err
	}

	ctx.Printf("Route removed: %s\n", host)
	return nil
}
