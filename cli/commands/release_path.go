package commands

import (
	"os"
	"os/user"
	"path/filepath"
)

const (
	systemReleasePath = "/var/lib/miren/release"
)

// FindReleasePath looks for an existing miren release directory.
// It checks the user's home directory first (~/.miren/release), then
// falls back to the system path (/var/lib/miren/release).
// Returns empty string if no release directory is found.
func FindReleasePath() string {
	// Check user release path first (respects user preference)
	if homeDir, err := getUserHomeDir(); err == nil {
		userPath := filepath.Join(homeDir, ".miren", "release")
		if _, err := os.Stat(userPath); err == nil {
			return userPath
		}
	}

	// Check system release path
	if _, err := os.Stat(systemReleasePath); err == nil {
		return systemReleasePath
	}

	return ""
}

// getUserHomeDir returns the user's home directory, handling the case
// where the command is run under sudo by checking SUDO_USER.
func getUserHomeDir() (string, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		// Running under sudo, get the original user's home
		u, err := user.Lookup(sudoUser)
		if err == nil {
			return u.HomeDir, nil
		}
		// Fallback to HOME env var
		if homeDir := os.Getenv("HOME"); homeDir != "" {
			return homeDir, nil
		}
	} else {
		// Not running under sudo
		if homeDir := os.Getenv("HOME"); homeDir != "" {
			return homeDir, nil
		}
		u, err := user.Current()
		if err != nil {
			return "", err
		}
		return u.HomeDir, nil
	}
	return "", nil
}
