package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func RouteWaf(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname for the route (e.g., example.com); omit and pass --default for the default route"`
	Default bool   `long:"default" description:"Apply to the default route (instead of a hostname)"`
	Level   int    `long:"level" description:"OWASP CRS paranoia level (1-4)" default:"1"`
	Disable bool   `long:"disable" description:"Disable WAF on the route"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Host == "" && !opts.Default {
		return fmt.Errorf("either a hostname or --default must be specified")
	}

	if opts.Host != "" && opts.Default {
		return fmt.Errorf("--default cannot be used with a hostname")
	}

	if !opts.Disable && (opts.Level < 1 || opts.Level > 4) {
		return fmt.Errorf("WAF level must be between 1 and 4, got %d", opts.Level)
	}

	client, err := routeSettingsRPCClient(ctx)
	if err != nil {
		return err
	}

	ic := ingress_v1alpha.NewRoutesClient(client)
	res, err := ic.Show(ctx, opts.Host, opts.Default)
	if err != nil {
		return err
	}
	routeLabel := opts.Host
	if opts.Default {
		routeLabel = "default"
	}

	type RouteWafJSON struct {
		Route    string `json:"route"`
		WafLevel int    `json:"waf_level"`
	}

	if opts.Disable {
		if !res.Route().WafEnabled() {
			if opts.IsJSON() {
				return PrintJSON(RouteWafJSON{Route: routeLabel, WafLevel: 0})
			}
			ctx.Printf("WAF is not enabled on route: %s\n", routeLabel)
			return nil
		}

		_, err = ic.Waf(ctx, opts.Host, opts.Default, 0)
		if err != nil {
			return fmt.Errorf("failed to disable WAF on route: %w", err)
		}

		if opts.IsJSON() {
			return PrintJSON(RouteWafJSON{Route: routeLabel, WafLevel: 0})
		}
		ctx.Printf("WAF disabled on route: %s\n", routeLabel)
		return nil
	}

	_, err = ic.Waf(ctx, opts.Host, opts.Default, int64(opts.Level))
	if err != nil {
		return fmt.Errorf("failed to enable WAF on route: %w", err)
	}

	if opts.IsJSON() {
		return PrintJSON(RouteWafJSON{Route: routeLabel, WafLevel: opts.Level})
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Route", routeLabel),
		ui.NewNamedValue("WAF Level", opts.Level),
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())

	return nil
}
