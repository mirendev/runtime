package deploygating

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/tasks"
)

// CheckDeployAllowed validates whether a deployment from the given directory is allowed.
// It returns a remedy string explaining how to fix any issues, and an error if the
// deployment should be blocked.
func CheckDeployAllowed(dir string) (string, error) {
	// Convert to absolute path for consistent checking
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve directory path: %w", err)
	}

	// Check if directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "Ensure you are running the deploy command from the correct directory.",
				fmt.Errorf("deployment directory does not exist: %s", absDir)
		}
		return "", fmt.Errorf("failed to access deployment directory: %w", err)
	}

	if !info.IsDir() {
		return "Provide a valid directory path for deployment.",
			fmt.Errorf("deployment path is not a directory: %s", absDir)
	}

	// Validate appconfig syntax if present
	_, err = appconfig.LoadAppConfigUnder(absDir)
	if err != nil {
		return fmt.Sprintf("Fix the configuration error in %s.", appconfig.AppConfigPath),
			fmt.Errorf("invalid app configuration: %w", err)
	}

	// Validate Procfile syntax if present
	procfilePath := filepath.Join(absDir, "Procfile")
	_, err = tasks.ParseFile(procfilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "Fix the syntax error in Procfile.",
			fmt.Errorf("failed to parse Procfile: %w", err)
	}

	// Stackbuild can now infer a reasonable default web service if none is defined
	// so we no longer block deployments without a web service.

	return "", nil
}
