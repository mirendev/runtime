package release

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VersionInfo contains version information for a binary
type VersionInfo struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildDate time.Time `json:"build_date"`
}

// GetCurrentVersion gets the version info of the currently installed binary
func GetCurrentVersion(binaryPath string) (VersionInfo, error) {
	// Try with --format=json first (new versions)
	cmd := exec.Command(binaryPath, "version", "--format=json")
	output, err := cmd.Output()
	if err == nil {
		// Try to parse as JSON
		var info VersionInfo
		if err := json.Unmarshal(output, &info); err == nil {
			return info, nil
		}
	}

	// Fall back to text parsing for older versions
	cmd = exec.Command(binaryPath, "version")
	output, err = cmd.Output()
	if err != nil {
		return VersionInfo{}, fmt.Errorf("failed to get version: %w", err)
	}

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
				dateStr := strings.TrimSpace(parts[1])
				// Try to parse the date
				if t, err := time.Parse("2006-01-02 15:04:05 MST", dateStr); err == nil {
					info.BuildDate = t
				} else if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
					info.BuildDate = t
				}
			}
		}
	}

	// Handle simple single-line version output (just the version string)
	if info.Version == "unknown" && len(lines) > 0 && lines[0] != "" && !strings.Contains(lines[0], ":") {
		info.Version = strings.TrimSpace(lines[0])
	}

	return info
}

// IsNewer returns true if this version is newer than the other
func (v VersionInfo) IsNewer(other VersionInfo) bool {
	// If both have build dates, use those for comparison
	if !v.BuildDate.IsZero() && !other.BuildDate.IsZero() {
		return v.BuildDate.After(other.BuildDate)
	}

	// If only one has a build date, consider it newer
	if !v.BuildDate.IsZero() && other.BuildDate.IsZero() {
		return true
	}
	if v.BuildDate.IsZero() && !other.BuildDate.IsZero() {
		return false
	}

	// Fall back to version string comparison
	// Don't upgrade if versions are the same
	if v.Version == other.Version {
		return false
	}

	// Otherwise, consider it newer if different
	return true
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
