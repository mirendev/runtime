package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SemVer represents a semantic version
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string // e.g., "test.1", "rc.1"
	Original   string // Original version string
}

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

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

	// Both have prereleases - compare per SemVer 2.0.0 spec
	if s.Prerelease != other.Prerelease {
		return comparePrereleaseSegments(s.Prerelease, other.Prerelease) > 0
	}

	// Versions are identical
	return false
}

// comparePrereleaseSegments compares prerelease versions per SemVer 2.0.0 spec
// Returns: 1 if a > b, -1 if a < b, 0 if equal
func comparePrereleaseSegments(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	// Compare each segment pair
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])

		if aErr == nil && bErr == nil {
			// Both numeric - compare as integers
			if aNum != bNum {
				if aNum > bNum {
					return 1
				}
				return -1
			}
		} else if aErr == nil {
			// a is numeric, b is not - numeric has lower precedence
			return -1
		} else if bErr == nil {
			// b is numeric, a is not - numeric has lower precedence
			return 1
		} else {
			// Both alphanumeric - compare lexicographically
			if aParts[i] != bParts[i] {
				if aParts[i] > bParts[i] {
					return 1
				}
				return -1
			}
		}
	}

	// All compared segments are equal - shorter prerelease has lower precedence
	if len(aParts) > len(bParts) {
		return 1
	} else if len(aParts) < len(bParts) {
		return -1
	}
	return 0
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
