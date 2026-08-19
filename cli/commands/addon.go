package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AddonListAvailable(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer cl.Close()

	eac := entityserver_v1alpha.NewEntityAccessClient(cl)

	kindRes, err := eac.LookupKind(ctx, "addon")
	if err != nil {
		return err
	}

	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		type addonInfo struct {
			Name           string   `json:"name"`
			DisplayName    string   `json:"display_name"`
			Description    string   `json:"description"`
			DefaultVariant string   `json:"default_variant"`
			DefaultVersion string   `json:"default_version"`
			Variants       []string `json:"variants"`
		}

		var addons []addonInfo
		for _, e := range res.Values() {
			var addon addon_v1alpha.Addon
			addon.Decode(e.Entity())
			var variantNames []string
			for _, v := range addon.Variants {
				variantNames = append(variantNames, v.Name)
			}
			addons = append(addons, addonInfo{
				Name:           addon.Name,
				DisplayName:    addon.DisplayName,
				Description:    addon.Description,
				DefaultVariant: addon.DefaultVariant,
				DefaultVersion: addon.DefaultVersion,
				Variants:       variantNames,
			})
		}
		return PrintJSON(addons)
	}

	var rows []ui.Row
	headers := []string{"ADDON", "DESCRIPTION", "VARIANTS", "DEFAULT VARIANT", "DEFAULT VERSION"}

	for _, e := range res.Values() {
		var addon addon_v1alpha.Addon
		addon.Decode(e.Entity())

		var variantNames []string
		for _, v := range addon.Variants {
			variantNames = append(variantNames, v.Name)
		}

		rows = append(rows, ui.Row{
			addon.Name,
			addon.Description,
			strings.Join(variantNames, ", "),
			addon.DefaultVariant,
			addon.DefaultVersion,
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No addons available\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows,
		ui.Columns().MaxWidth(2, 30).WordWrap(2))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

func AddonVariants(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Addon string `position:"0" usage:"Addon name (e.g., miren-postgresql)" required:"true"`
}) error {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer cl.Close()

	eac := entityserver_v1alpha.NewEntityAccessClient(cl)

	addonRes, err := eac.Get(ctx, "addon/"+opts.Addon)
	if err != nil {
		return fmt.Errorf("addon %q not found: %w", opts.Addon, err)
	}

	var addon addon_v1alpha.Addon
	addon.Decode(addonRes.Entity().Entity())

	if opts.IsJSON() {
		type variantInfo struct {
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Details     map[string]string `json:"details,omitempty"`
			Default     bool              `json:"default,omitempty"`
		}

		var variants []variantInfo
		for _, v := range addon.Variants {
			details := make(map[string]string)
			for _, d := range v.Details {
				details[d.Key] = d.Value
			}
			variants = append(variants, variantInfo{
				Name:        v.Name,
				Description: v.Description,
				Details:     details,
				Default:     v.Name == addon.DefaultVariant,
			})
		}
		return PrintJSON(variants)
	}

	ctx.Printf("Variants for %s:\n\n", addon.DisplayName)

	var rows []ui.Row
	headers := []string{"VARIANT", "DESCRIPTION", "DEFAULT"}

	for _, v := range addon.Variants {
		def := ""
		if v.Name == addon.DefaultVariant {
			def = "yes"
		}
		rows = append(rows, ui.Row{
			v.Name,
			v.Description,
			def,
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No variants available\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())

	for _, v := range addon.Variants {
		if len(v.Details) > 0 {
			ctx.Printf("\n%s:\n", v.Name)
			for _, d := range v.Details {
				ctx.Printf("  %s: %s\n", d.Key, d.Value)
			}
		}
	}

	return nil
}

func AddonCreate(ctx *Context, opts struct {
	AppCentric
	Spec    string `position:"0" usage:"Addon spec (e.g., miren-postgresql:small)" required:"true"`
	Version string `long:"version" short:"V" description:"Software version or full image reference (e.g., 16, or registry.example.com/postgres:16-custom)"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/addons")
	if err != nil {
		return err
	}

	addonsClient := app_v1alpha.NewAddonsClient(cl)

	result, err := addonsClient.CreateInstance(ctx, "", opts.Spec, "", opts.App, opts.Version, nil)
	if err != nil {
		return err
	}

	ctx.Completed("Addon attached to %s (id: %s)", opts.App, ui.CleanEntityID(result.Id()))
	return nil
}

func AddonList(ctx *Context, opts struct {
	FormatOptions
	AppCentric
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/addons")
	if err != nil {
		return err
	}

	addonsClient := app_v1alpha.NewAddonsClient(cl)

	res, err := addonsClient.ListInstances(ctx, opts.App)
	if err != nil {
		return err
	}

	addons := res.Addons()

	if opts.IsJSON() {
		type addonInfo struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Variant string `json:"variant"`
			Version string `json:"version,omitempty"`
		}

		var infos []addonInfo
		for _, a := range addons {
			infos = append(infos, addonInfo{
				ID:      a.Id(),
				Name:    a.Name(),
				Variant: a.Variant(),
				Version: a.Version(),
			})
		}
		return PrintJSON(infos)
	}

	var rows []ui.Row
	headers := []string{"ADDON", "VARIANT", "VERSION"}

	for _, a := range addons {
		version := a.Version()
		if version == "" {
			version = "(default)"
		}
		rows = append(rows, ui.Row{
			a.Name(),
			a.Variant(),
			version,
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No addons attached to %s\n", opts.App)
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

func AddonDestroy(ctx *Context, opts struct {
	AppCentric
	Name  string `position:"0" usage:"Addon name (e.g., miren-postgresql)" required:"true"`
	Force bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
}) error {
	if !opts.Force {
		confirmed, err := ui.Confirm(
			ui.WithMessage(fmt.Sprintf("This will destroy the %s addon and delete its data. Continue?", opts.Name)),
			ui.WithDefault(false),
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Printf("Aborted\n")
			return nil
		}
	}

	cl, err := ctx.RPCClient("dev.miren.runtime/addons")
	if err != nil {
		return err
	}

	addonsClient := app_v1alpha.NewAddonsClient(cl)

	_, err = addonsClient.DeleteInstance(ctx, opts.App, opts.Name)
	if err != nil {
		return err
	}

	ctx.Completed("Addon %s removed from %s", opts.Name, opts.App)
	return nil
}

func AddonRotate(ctx *Context, opts struct {
	AppCentric
	Name       string `position:"0" usage:"Addon name (e.g., miren-valkey)" required:"true"`
	Credential string `long:"credential" description:"Which credential to rotate (provider-specific; defaults to the addon's primary credential)"`
	Force      bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
}) error {
	if !opts.Force {
		confirmed, err := ui.Confirm(
			ui.WithMessage(fmt.Sprintf("This will rotate the %s credential and redeploy consuming apps. Continue?", opts.Name)),
			ui.WithDefault(false),
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Printf("Aborted\n")
			return nil
		}
	}

	cl, err := ctx.RPCClient("dev.miren.runtime/addons")
	if err != nil {
		return err
	}

	addonsClient := app_v1alpha.NewAddonsClient(cl)

	res, err := addonsClient.RotateCredential(ctx, opts.App, opts.Name, opts.Credential)
	if err != nil {
		return err
	}

	ctx.Completed("Rotation requested for %s on %s (request %s)", opts.Name, opts.App, res.Id())
	return nil
}
