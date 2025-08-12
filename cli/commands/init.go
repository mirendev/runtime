package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/appconfig"
)

func Init(ctx *Context, opts struct {
	Name string `short:"n" long:"name" description:"Application name (defaults to directory name)"`
	Dir  string `short:"d" long:"dir" description:"Application directory (defaults to current directory)"`
	ConfigCentric
}) error {
	// Determine working directory
	workDir := opts.Dir
	if workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		workDir = wd
	} else {
		// Convert to absolute path
		absDir, err := filepath.Abs(workDir)
		if err != nil {
			return fmt.Errorf("failed to resolve directory path: %w", err)
		}
		workDir = absDir
	}

	// Determine app name
	appName := opts.Name
	if appName == "" {
		// Use directory name as default
		appName = filepath.Base(workDir)

		// Sanitize the app name - replace spaces and special chars with hyphens
		appName = strings.ToLower(appName)
		appName = strings.ReplaceAll(appName, " ", "-")
		appName = strings.ReplaceAll(appName, "_", "-")
	}

	// Check if already initialized BEFORE creating the app
	appTomlPath := filepath.Join(workDir, appconfig.AppConfigPath)
	runtimeDir := filepath.Dir(appTomlPath)
	if _, err := os.Stat(appTomlPath); err == nil {
		return fmt.Errorf("app.toml already exists in %s - app already initialized", runtimeDir)
	}

	// Create app client
	rpcClient, err := ctx.RPCClient("entities")
	if err != nil {
		return fmt.Errorf("failed to connect to entity server: %w", err)
	}

	appClient := app.NewClient(ctx.Log, rpcClient)

	// Check if app already exists
	_, err = appClient.GetByName(ctx, appName)
	if err == nil {
		// App exists if GetByName succeeds
		return fmt.Errorf("app '%s' already exists", appName)
	}

	// Create the app
	appEntity, err := appClient.Create(ctx, appName)
	if err != nil {
		return fmt.Errorf("could not create app '%s': %w", appName, err)
	}

	// From this point on, if we fail we need to rollback
	var initErr error
	defer func() {
		if initErr != nil {
			if rollbackErr := rollbackInit(ctx, appName, workDir); rollbackErr != nil {
				ctx.Printf("Warning: Could not rollback init: %v\n", rollbackErr)
			}
		}
	}()

	// Create .miren directory
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		initErr = fmt.Errorf("failed to create .miren directory: %w", err)
		return initErr
	}

	// Create app config with just the name
	appConfig := &appconfig.AppConfig{
		Name: appName,
	}

	// Marshal to TOML
	content, err := toml.Marshal(appConfig)
	if err != nil {
		initErr = fmt.Errorf("failed to marshal app config: %w", err)
		return initErr
	}

	// Write app.toml
	if err := os.WriteFile(appTomlPath, content, 0644); err != nil {
		initErr = fmt.Errorf("failed to write app.toml: %w", err)
		return initErr
	}

	// Success - no rollback needed

	ctx.Printf("Initialized Miren app '%s' in %s\n", appName, appTomlPath)
	ctx.Printf("Created app '%s' with id: %s\n", appName, appEntity.ID)
	return nil
}

// if the app creation fails at any point, we want to cleanup.
func rollbackInit(ctx *Context, appName string, workDir string) error {
	// Create app client
	rpcClient, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	appClient := app.NewClient(ctx.Log, rpcClient)

	// Try to destroy the app
	err = appClient.Destroy(ctx, appName)
	if err != nil {
		ctx.Printf("Could not delete app on rollback: %v\n", err)
	}

	// Derive runtime directory from centralized config path
	appTomlPath := filepath.Join(workDir, appconfig.AppConfigPath)
	runtimeDir := filepath.Dir(appTomlPath)
	if _, err := os.Stat(runtimeDir); err == nil {
		err := os.RemoveAll(runtimeDir)
		if err != nil {
			ctx.Printf("Failed to remove .miren directory on rollback: %v\n", err)
		}
	}
	return nil
}
