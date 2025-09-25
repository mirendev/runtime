package release

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// VersionInfo contains version information for a binary
type VersionInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// GetCurrentVersion gets the version info of the currently installed binary
func GetCurrentVersion(binaryPath string) (VersionInfo, error) {
	cmd := exec.Command(binaryPath, "version", "--json")
	output, err := cmd.Output()
	if err != nil {
		// Try without --json flag for older versions
		cmd = exec.Command(binaryPath, "version")
		output, err = cmd.Output()
		if err != nil {
			return VersionInfo{}, fmt.Errorf("failed to get version: %w", err)
		}
		// Parse simple text output
		return parseVersionText(string(output)), nil
	}
	// Future: parse JSON output when we add --json support
	return parseVersionText(string(output)), nil
}

// parseVersionText parses version output text
func parseVersionText(output string) VersionInfo {
	info := VersionInfo{
		Version: "unknown",
		Commit:  "unknown",
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Version:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				info.Version = strings.TrimSpace(parts[1])
			}
		} else if strings.Contains(line, "Commit:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				info.Commit = strings.TrimSpace(parts[1])
			}
		} else if strings.Contains(line, "Built:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				info.BuildDate = strings.TrimSpace(parts[1])
			}
		}
	}

	return info
}

// GetBinaryPath returns the path to the miren binary
func GetBinaryPath() string {
	// Check if we're running from a release directory
	exe, err := os.Executable()
	if err == nil {
		return exe
	}

	// Default to system path
	return "/var/lib/miren/release/miren"
}
