package release

import (
	"fmt"
	"regexp"
	"strconv"
)

// SemVer represents a semantic version
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string // e.g., "test.1", "rc.1"
	Original   string // Original version string
}

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z\-\.]+))?$`)

// ParseSemVer parses a semantic version string
// Examples: "v1.2.3", "v0.1.0-test.1", "1.0.0-rc.1"
func ParseSemVer(version string) (*SemVer, error) {
	matches := semverRegex.FindStringSubmatch(version)
	if matches == nil {
		return nil, fmt.Errorf("invalid semver format: %s", version)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	prerelease := matches[4]

	return &SemVer{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: prerelease,
		Original:   version,
	}, nil
}

// IsNewer returns true if this semver is newer than other
func (s *SemVer) IsNewer(other *SemVer) bool {
	// Compare major version
	if s.Major != other.Major {
		return s.Major > other.Major
	}

	// Compare minor version
	if s.Minor != other.Minor {
		return s.Minor > other.Minor
	}

	// Compare patch version
	if s.Patch != other.Patch {
		return s.Patch > other.Patch
	}

	// Handle prerelease comparison
	// No prerelease is newer than with prerelease
	if s.Prerelease == "" && other.Prerelease != "" {
		return true
	}
	if s.Prerelease != "" && other.Prerelease == "" {
		return false
	}

	// Both have prereleases - compare lexicographically
	if s.Prerelease != other.Prerelease {
		return s.Prerelease > other.Prerelease
	}

	// Versions are identical
	return false
}

// IsPrerelease returns true if this is a prerelease version
func (s *SemVer) IsPrerelease() bool {
	return s.Prerelease != ""
}

// String returns the string representation
func (s *SemVer) String() string {
	if s.Original != "" {
		return s.Original
	}
	if s.Prerelease != "" {
		return fmt.Sprintf("v%d.%d.%d-%s", s.Major, s.Minor, s.Patch, s.Prerelease)
	}
	return fmt.Sprintf("v%d.%d.%d", s.Major, s.Minor, s.Patch)
}
