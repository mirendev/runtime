package commands

import (
	"fmt"
	"os"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AuthProviderAddPassword(ctx *Context, opts struct {
	Name     string `position:"0" usage:"Name for this password provider" required:"true"`
	Password string `long:"password" description:"Password (omit to prompt interactively, use @file to read from file)"`
	Update   bool   `long:"update" description:"Overwrite an existing provider with the same name (rotates password)"`
	ConfigCentric
}) error {
	if opts.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	password := opts.Password

	switch {
	case password == "":
		prompted, err := ui.PromptForInput(
			ui.WithLabel("Enter password"),
			ui.WithSensitive(true),
		)
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		if prompted == "" {
			return fmt.Errorf("password cannot be empty")
		}
		password = prompted
	case strings.HasPrefix(password, "@"):
		data, err := os.ReadFile(password[1:])
		if err != nil {
			return fmt.Errorf("failed to read password from file %s: %w", password[1:], err)
		}
		password = strings.TrimRight(string(data), "\r\n")
		if password == "" {
			return fmt.Errorf("password file is empty")
		}
	}

	client, err := providerRPCClient(ctx)
	if err != nil {
		return err
	}

	_, err = ingress_v1alpha.NewIdentityProvidersClient(client).AddPassword(ctx, opts.Name, password, opts.Update)
	if err != nil {
		return fmt.Errorf("failed to create password provider: %w", err)
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Name", opts.Name),
		ui.NewNamedValue("Type", "password"),
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	return nil
}
