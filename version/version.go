package version

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Build-time variables set via -ldflags
var (
	Version   = "unknown"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info represents version information
type Info struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildDate time.Time `json:"build_date"`
}

// GetInfo returns the current version information
func GetInfo() Info {
	info := Info{
		Version: Version,
		Commit:  Commit,
	}

	// Parse build date if available
	if BuildDate != "unknown" && BuildDate != "" {
		if t, err := time.Parse(time.RFC3339, BuildDate); err == nil {
			info.BuildDate = t.UTC()
		}
	}

	return info
}

// Returns the branch that the version was built from
func Branch() string {
	if Version == "unknown" {
		return "unknown"
	}

	if branch, _, ok := strings.Cut(Version, ":"); ok {
		return branch
	}

	if strings.HasPrefix(Version, "v") {
		return Version
	}

	return "unknown"
}

// String returns the version info as a formatted string
func (i Info) String() string {
	if i.Version == "unknown" {
		return "unknown"
	}

	s := fmt.Sprintf("Version: %s", i.Version)
	if i.Commit != "unknown" && i.Commit != "" {
		s += fmt.Sprintf("\nCommit:  %s", i.Commit)
	}
	if !i.BuildDate.IsZero() {
		s += fmt.Sprintf("\nBuilt:   %s", i.BuildDate.Format("2006-01-02 15:04:05 UTC"))
	}
	return s
}

// JSON returns the version info as JSON
func (i Info) JSON() (string, error) {
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// IsNewer returns true if this version info is newer than the other
func (i Info) IsNewer(other Info) bool {
	// If both have build dates, use those for comparison
	if !i.BuildDate.IsZero() && !other.BuildDate.IsZero() {
		return i.BuildDate.After(other.BuildDate)
	}

	// If only one has a build date, consider it newer
	if !i.BuildDate.IsZero() && other.BuildDate.IsZero() {
		return true
	}
	// Fall back to version string comparison
	// This is simple and won't handle semantic versioning perfectly,
	// but works for branch:commit format and simple versions
	return i.Version != other.Version
}
