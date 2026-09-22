package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/ui"
)

func RouteTLSCheck(ctx *Context, opts struct {
	Host  string `position:"0" usage:"Hostname for the route (e.g., *.example.com)"`
	Path  string `position:"1" usage:"Path on the app to ask before issuing a certificate (e.g., /tls-check); omit to show the current value"`
	Clear bool   `long:"clear" description:"Remove the check so names under the route only get certificates as live ephemeral deploys"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Host == "" {
		return fmt.Errorf("a hostname is required")
	}
	if opts.Clear && opts.Path != "" {
		return fmt.Errorf("--clear cannot be used with a path")
	}
	if opts.Path != "" {
		if err := ingress.ValidateTLSCheckPath(opts.Path); err != nil {
			return err
		}
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	route, err := ic.Lookup(ctx, opts.Host)
	if err != nil {
		return fmt.Errorf("failed to lookup route: %w", err)
	}
	if route == nil {
		return fmt.Errorf("route not found for host: %s", opts.Host)
	}

	type RouteTLSCheckJSON struct {
		Route    string `json:"route"`
		TLSCheck string `json:"tls_check,omitempty"`
	}

	switch {
	case opts.Clear:
		if route.TlsCheck == "" {
			if opts.IsJSON() {
				return PrintJSON(RouteTLSCheckJSON{Route: opts.Host})
			}
			ctx.Printf("No TLS check on route: %s\n", opts.Host)
			return nil
		}

		if _, err := ic.ClearRouteTLSCheck(ctx, route); err != nil {
			return err
		}

		if opts.IsJSON() {
			return PrintJSON(RouteTLSCheckJSON{Route: opts.Host})
		}
		ctx.Printf("TLS check cleared on route: %s\n", opts.Host)
		return nil

	case opts.Path != "":
		updated, err := ic.SetRouteTLSCheck(ctx, route, opts.Path)
		if err != nil {
			return err
		}
		route = updated

	default:
		if route.TlsCheck == "" {
			if opts.IsJSON() {
				return PrintJSON(RouteTLSCheckJSON{Route: opts.Host})
			}
			ctx.Printf("Route %s has no TLS check\n", opts.Host)
			return nil
		}
	}

	if opts.IsJSON() {
		return PrintJSON(RouteTLSCheckJSON{Route: opts.Host, TLSCheck: route.TlsCheck})
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Route", opts.Host),
		ui.NewNamedValue("TLS Check", route.TlsCheck),
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())

	return nil
}
