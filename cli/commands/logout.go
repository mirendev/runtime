package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// Logout removes the local identity and key files
func Logout(ctx *Context, opts struct {
	ConfigCentric
	IdentityName string `short:"i" long:"identity" description:"Name of the identity to remove"`
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
	identityNames := cfg.GetIdentityNames()

	// If no identity specified, prompt or auto-select
	if identityName == "" {
		if len(identityNames) == 0 {
			ctx.Info("No identities configured. Nothing to logout from.")
			return nil
		} else if len(identityNames) == 1 {
			// Auto-select the only identity
			identityName = identityNames[0]
		} else if ui.IsInteractive() {
			// Multiple identities - show picker with user info
			items := make([]ui.PickerItem, len(identityNames))
			for i, name := range identityNames {
				label := name
				if userInfo := getIdentityUserInfo(ctx, cfg, name); userInfo != "" {
					label = fmt.Sprintf("%s - %s", name, userInfo)
				}
				items[i] = ui.SimplePickerItem{Text: label}
			}

			selected, err := ui.RunPicker(items,
				ui.WithTitle("Select identity to logout:"),
			)
			if err != nil {
				return fmt.Errorf("failed to run picker: %w", err)
			}
			if selected == nil {
				return fmt.Errorf("cancelled")
			}

			// Extract identity name from selection (before the " - ")
			identityName = identityNames[0]
			for _, name := range identityNames {
				if selected.ID() == name || strings.HasPrefix(selected.ID(), name+" - ") {
					identityName = name
					break
				}
			}
		} else {
			// Non-interactive with multiple identities
			return fmt.Errorf("multiple identities configured; use --identity to specify which one: %v", identityNames)
		}
	}

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

	// For ephemeral token identities, best-effort revoke the refresh token so a
	// leaked ~/.config copy can't keep renewing after logout. Never let a failed
	// revoke block the local logout.
	if identity.Type == "token" && identity.RefreshToken != "" {
		if err := clientconfig.RevokeRefreshToken(ctx, identity.Issuer, identity.Token, identity.RefreshToken); err != nil {
			ctx.Warn("Could not revoke refresh token on the server (removing local credentials anyway): %v", err)
		} else {
			ctx.Info("Revoked refresh token on the server")
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
