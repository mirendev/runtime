package release

import (
	"testing"
	"time"
)

func TestVersionInfo_IsNewer(t *testing.T) {
	now := time.Now()
	later := now.Add(5 * time.Minute)

	tests := []struct {
		name     string
		v        VersionInfo
		other    VersionInfo
		expected bool
	}{
		{
			name: "same commit different build times - not newer",
			v: VersionInfo{
				Version:   "main:abc123",
				Commit:    "abc123def456",
				BuildDate: later,
			},
			other: VersionInfo{
				Version:   "main:abc123",
				Commit:    "abc123def456",
				BuildDate: now,
			},
			expected: false,
		},
		{
			name: "different commits newer build time - newer",
			v: VersionInfo{
				Version:   "main:def456",
				Commit:    "def456abc123",
				BuildDate: later,
			},
			other: VersionInfo{
				Version:   "main:abc123",
				Commit:    "abc123def456",
				BuildDate: now,
			},
			expected: true,
		},
		{
			name: "different commits older build time - not newer",
			v: VersionInfo{
				Version:   "main:def456",
				Commit:    "def456abc123",
				BuildDate: now,
			},
			other: VersionInfo{
				Version:   "main:abc123",
				Commit:    "abc123def456",
				BuildDate: later,
			},
			expected: false,
		},
		{
			name: "no commits different build times - newer by build time",
			v: VersionInfo{
				Version:   "main:abc123",
				BuildDate: later,
			},
			other: VersionInfo{
				Version:   "main:abc123",
				BuildDate: now,
			},
			expected: true,
		},
		{
			name: "no commits same build time same version - not newer",
			v: VersionInfo{
				Version:   "main:abc123",
				BuildDate: now,
			},
			other: VersionInfo{
				Version:   "main:abc123",
				BuildDate: now,
			},
			expected: false,
		},
		{
			name: "unknown commits different build times - newer by build time",
			v: VersionInfo{
				Version:   "main:abc123",
				Commit:    "unknown",
				BuildDate: later,
			},
			other: VersionInfo{
				Version:   "main:abc123",
				Commit:    "unknown",
				BuildDate: now,
			},
			expected: true,
		},
		{
			name: "one has build date other doesn't - newer",
			v: VersionInfo{
				Version:   "main:abc123",
				Commit:    "abc123def456",
				BuildDate: now,
			},
			other: VersionInfo{
				Version: "main:abc123",
				Commit:  "def456abc123",
			},
			expected: true,
		},
		{
			name: "same version no commits no build dates - not newer",
			v: VersionInfo{
				Version: "main:abc123",
			},
			other: VersionInfo{
				Version: "main:abc123",
			},
			expected: false,
		},
		{
			name: "different versions no other info - newer",
			v: VersionInfo{
				Version: "main:def456",
			},
			other: VersionInfo{
				Version: "main:abc123",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v.IsNewer(tt.other)
			if got != tt.expected {
				t.Errorf("IsNewer() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
