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

// Branch returns the release channel this binary was built from: the tag name
// for tagged releases (e.g. "v0.2.0"), the branch name for branch builds
// (e.g. "main" for "main:abc123"), and empty for unknown versions. See BranchOf.
func Branch() string {
	return BranchOf(Version)
}

// BranchOf derives the release channel from a version string as produced by
// hack/build.sh. A detached checkout reports its branch as "HEAD", which is not
// a channel anything publishes under; it is treated as "main" so installers
// pick the main image and release bundle rather than failing on a channel
// named HEAD.
func BranchOf(v string) string {
	branch, _, ok := strings.Cut(v, ":")
	if !ok {
		if v == "unknown" {
			return ""
		}
		branch = v
	}
	if branch == "HEAD" {
		return "main"
	}
	return branch
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
