package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

// resolveRouteTimeoutArgs validates the argument combination before any RPC
// work and returns the host and timeout to act on.
//
// With --default there is no hostname to give, so the sole positional is the
// duration: `route timeout --default 5m` arrives as host="5m", timeout="", and
// is shifted into place here.
func resolveRouteTimeoutArgs(host string, isDefault, clear bool, timeout string) (string, string, error) {
	if isDefault && host != "" && timeout == "" {
		host, timeout = "", host
	}

	if host == "" && !isDefault {
		return "", "", fmt.Errorf("either a hostname or --default must be specified")
	}

	if host != "" && isDefault {
		return "", "", fmt.Errorf("--default cannot be used with a hostname")
	}

	if clear && timeout != "" {
		return "", "", fmt.Errorf("--clear cannot be used with a timeout value")
	}

	if timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return "", "", fmt.Errorf("invalid timeout %q: expected a duration like 10m or 300s", timeout)
		}
		if d <= 0 {
			return "", "", fmt.Errorf("timeout must be positive, got %q", timeout)
		}
	}

	return host, timeout, nil
}

func RouteTimeout(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com); omit and pass --default for the default route"`
	Timeout string `position:"1" usage:"Request timeout as a duration (e.g., 10m, 300s); omit to show the current value"`
	Default bool   `long:"default" description:"Apply to the default route (instead of a hostname)"`
	Clear   bool   `long:"clear" description:"Remove the override so the route uses the server default"`
	FormatOptions
	ConfigCentric
}) error {
	host, wantTimeout, err := resolveRouteTimeoutArgs(opts.Host, opts.Default, opts.Clear, opts.Timeout)
	if err != nil {
		return err
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	var route *ingress_v1alpha.HttpRoute
	var routeLabel string

	if opts.Default {
		route, err = ic.LookupDefault(ctx)
		if err != nil {
			return fmt.Errorf("failed to lookup default route: %w", err)
		}
		if route == nil {
			return fmt.Errorf("no default route configured")
		}
		routeLabel = "default"
	} else {
		route, err = ic.Lookup(ctx, host)
		if err != nil {
			return fmt.Errorf("failed to lookup route: %w", err)
		}
		if route == nil {
			return fmt.Errorf("route not found for host: %s", host)
		}
		routeLabel = host
	}

	type RouteTimeoutJSON struct {
		Route          string `json:"route"`
		RequestTimeout string `json:"request_timeout,omitempty"`
	}

	switch {
	case opts.Clear:
		if route.RequestTimeout == "" {
			if opts.IsJSON() {
				return PrintJSON(RouteTimeoutJSON{Route: routeLabel})
			}
			ctx.Printf("No request timeout override on route: %s\n", routeLabel)
			return nil
		}

		if _, err := ic.ClearRouteRequestTimeout(ctx, route); err != nil {
			return fmt.Errorf("failed to clear request timeout on route: %w", err)
		}

		if opts.IsJSON() {
			return PrintJSON(RouteTimeoutJSON{Route: routeLabel})
		}
		ctx.Printf("Request timeout cleared on route: %s (now using the server default)\n", routeLabel)
		return nil

	case wantTimeout != "":
		updated, err := ic.SetRouteRequestTimeout(ctx, route, wantTimeout)
		if err != nil {
			return fmt.Errorf("failed to set request timeout on route: %w", err)
		}
		route = updated

	default:
		// No value given — report the current setting.
		if route.RequestTimeout == "" {
			if opts.IsJSON() {
				return PrintJSON(RouteTimeoutJSON{Route: routeLabel})
			}
			ctx.Printf("Route %s uses the server default request timeout\n", routeLabel)
			return nil
		}
	}

	if opts.IsJSON() {
		return PrintJSON(RouteTimeoutJSON{Route: routeLabel, RequestTimeout: route.RequestTimeout})
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Route", routeLabel),
		ui.NewNamedValue("Timeout", route.RequestTimeout),
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())

	return nil
}
