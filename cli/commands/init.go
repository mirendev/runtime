package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"miren.dev/runtime/api/app/app_v1alpha"
)

func Init(ctx *Context, opts struct {
	Name string `short:"n" long:"name" description:"Application name (defaults to current directory name)"`
}) error {
	// Determine app name
	appName := opts.Name
	if appName == "" {
		// Use current directory name as default
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		appName = filepath.Base(wd)

		// Sanitize the app name - replace spaces and special chars with hyphens
		appName = strings.ToLower(appName)
		appName = strings.ReplaceAll(appName, " ", "-")
		appName = strings.ReplaceAll(appName, "_", "-")
	}

	// Check if already initialized BEFORE creating the app
	runtimeDir := ".runtime"
	appTomlPath := filepath.Join(runtimeDir, "app.toml")
	if _, err := os.Stat(appTomlPath); err == nil {
		return fmt.Errorf("app.toml already exists in .runtime directory - app already initialized")
	}

	// Create the app entity in the runtime
	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	crudClient := app_v1alpha.NewCrudClient(crudcl)

	// Check if app already exists
	_, err = crudClient.GetApp(ctx, appName)
	if err == nil {
		// App exists if GetApp succeeds
		return fmt.Errorf("app '%s' already exists", appName)
	}

	// Create the app
	results, err := crudClient.New(ctx, appName)
	if err != nil {
		return fmt.Errorf("could not create app '%s': %w", appName, err)
	}

	// From this point on, if we fail we need to rollback
	var initErr error
	defer func() {
		if initErr != nil {
			if rollbackErr := rollbackInit(ctx, appName); rollbackErr != nil {
				ctx.Printf("Warning: Could not rollback init: %v\n", rollbackErr)
			}
		}
	}()

	// Create .runtime directory
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		initErr = fmt.Errorf("failed to create .runtime directory: %w", err)
		return initErr
	}

	// Write app.toml with just the name
	content := fmt.Sprintf("name = \"%s\"\n", appName)
	if err := os.WriteFile(appTomlPath, []byte(content), 0644); err != nil {
		initErr = fmt.Errorf("failed to write app.toml: %w", err)
		return initErr
	}

	// Success - no rollback needed

	ctx.Printf("Initialized runtime app '%s' in .runtime/app.toml\n", appName)
	ctx.Printf("Created app '%s' with id: %s\n", appName, results.Id())
	return nil
}

// if the app creation fails at any point, we want to cleanup.
func rollbackInit(ctx *Context, appName string) error {
	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}
	crudClient := app_v1alpha.NewCrudClient(crudcl)
	_, err = crudClient.Destroy(ctx, appName)
	if err != nil {
		ctx.Printf("Could not delete app on rollback: %v\n", err)
	}

	runtimeDir := ".runtime"
	if _, err := os.Stat(runtimeDir); err == nil {
		err := os.RemoveAll(runtimeDir)
		if err != nil {
			ctx.Printf("Failed to remove .runtime directory on rollback: %v\n", err)
		}
	}
	return nil
}
