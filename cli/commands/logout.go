package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/clientconfig"
)

// Logout removes the local identity and key files
func Logout(ctx *Context, opts struct {
	ConfigCentric
	IdentityName string `short:"i" long:"identity" description:"Name of the identity to remove" default:"cloud"`
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		// If no config exists, nothing to logout from
		if err == clientconfig.ErrNoConfig {
			ctx.Info("No configuration found. Nothing to logout from.")
			return nil
		}
		return err
	}

	identityName := opts.IdentityName

	// Check if the identity exists
	identity, err := cfg.GetIdentity(identityName)
	if err != nil {
		return fmt.Errorf("identity %q not found", identityName)
	}

	// Get the config directory path
	configDir, err := getConfigDirPath()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}
	if configDir == "" {
		return fmt.Errorf("cannot logout: config is a single file, not a directory")
	}

	// Build file paths
	identityFile := filepath.Join(configDir, fmt.Sprintf("identity-%s.yaml", identityName))
	keyFile := ""
	if identity.KeyRef != "" {
		keyFile = filepath.Join(configDir, fmt.Sprintf("key-%s.yaml", identity.KeyRef))
	}

	// Check if any other identities reference this key
	keyInUse := false
	if keyFile != "" {
		for _, name := range cfg.GetIdentityNames() {
			if name == identityName {
				continue
			}
			otherIdentity, err := cfg.GetIdentity(name)
			if err != nil {
				continue
			}
			if otherIdentity.KeyRef == identity.KeyRef {
				keyInUse = true
				ctx.Warn("Key %q is also used by identity %q, not deleting key file", identity.KeyRef, name)
				break
			}
		}
	}

	// Delete the identity file
	if err := os.Remove(identityFile); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete identity file: %w", err)
		}
		ctx.Warn("Identity file not found: %s", identityFile)
	} else {
		ctx.Info("Deleted identity file: %s", identityFile)
	}

	// Delete the key file if not in use by other identities
	if keyFile != "" && !keyInUse {
		if err := os.Remove(keyFile); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to delete key file: %w", err)
			}
			ctx.Warn("Key file not found: %s", keyFile)
		} else {
			ctx.Info("Deleted key file: %s", keyFile)
		}
	}

	ctx.Completed("Logged out of identity %q", identityName)
	return nil
}

// getConfigDirPath returns the path to the clientconfig.d directory
func getConfigDirPath() (string, error) {
	// Check environment variable first
	if envPath := os.Getenv(clientconfig.EnvConfigPath); envPath != "" {
		info, err := os.Stat(envPath)
		if err == nil {
			if !info.IsDir() {
				// It's a file, don't use clientconfig.d
				return "", nil
			}
			return filepath.Join(envPath, "clientconfig.d"), nil
		}
	}

	// Use default XDG config directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}
	return filepath.Join(configDir, "miren", "clientconfig.d"), nil
}
