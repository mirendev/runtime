package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
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

	// Check if already initialized
	appTomlPath := filepath.Join(workDir, appconfig.AppConfigPath)
	runtimeDir := filepath.Dir(appTomlPath)
	if _, err := os.Stat(appTomlPath); err != nil {
		if !os.IsNotExist(err) {
			// Return unexpected errors (permission denied, IO errors, etc.)
			return fmt.Errorf("failed to check for existing app.toml: %w", err)
		}
		// File doesn't exist, continue with initialization
	} else {
		// File exists
		return fmt.Errorf("app.toml already exists in %s - app already initialized", runtimeDir)
	}

	// Create .miren directory
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .miren directory: %w", err)
	}

	// Create app config with just the name
	appConfig := &appconfig.AppConfig{
		Name: appName,
	}

	// Marshal to TOML
	content, err := toml.Marshal(appConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal app config: %w", err)
	}

	// Write app.toml
	if err := os.WriteFile(appTomlPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write app.toml: %w", err)
	}

	ctx.Printf("Initialized Miren app '%s' in %s\n", appName, appTomlPath)
	return nil
}
