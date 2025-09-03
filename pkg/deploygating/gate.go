package deploygating

import (
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

	// Check for web service definition
	hasWebService := false

	// First check .miren/app.toml for services
	ac, err := appconfig.LoadAppConfigUnder(absDir)
	if err != nil {
		return "", fmt.Errorf("failed to load app config: %w", err)
	}

	if ac != nil && ac.Services != nil {
		if _, ok := ac.Services["web"]; ok {
			hasWebService = true
		}
	}

	// If not found in app.toml, check Procfile
	if !hasWebService {
		procfilePath := filepath.Join(absDir, "Procfile")
		if _, err := os.Stat(procfilePath); err == nil {
			pf, err := tasks.ParseFile(procfilePath)
			if err != nil {
				return "", fmt.Errorf("failed to parse Procfile: %w", err)
			}

			for _, proc := range pf.Proceses {
				if proc.Name == "web" {
					hasWebService = true
					break
				}
			}
		}
	}

	// Return error if no web service is defined
	if !hasWebService {
		remedy := `To fix this, define a 'web' service in one of these ways:

Option 1: Add to .miren/app.toml:
  [services.web]
  command = "your-web-server-command"

Option 2: Add to Procfile:
  web: your-web-server-command

In both cases, your app should respect the env var PORT to bind to the correct port.
`

		return remedy, fmt.Errorf("no 'web' service defined in .miren/app.toml or Procfile")
	}

	// All checks passed
	return "", nil
}
