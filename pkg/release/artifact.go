package release

import (
	"fmt"
	"net/url"
	"runtime"
)

// ArtifactType represents the type of release artifact
type ArtifactType string

const (
	// ArtifactTypeBinary contains just the miren binary (.zip)
	ArtifactTypeBinary ArtifactType = "binary"
	// ArtifactTypeBase contains miren plus supporting binaries (.tar.gz, Linux only)
	ArtifactTypeBase ArtifactType = "base"
	// ArtifactTypeRelease is deprecated, use ArtifactTypeBase
	ArtifactTypeRelease ArtifactType = "release"
)

// Artifact represents a downloadable Miren component
type Artifact struct {
	Type     ArtifactType
	Version  string // Branch name (e.g., "main") or version tag
	Arch     string
	Platform string
}

// NewArtifact creates a new artifact descriptor with current platform defaults
func NewArtifact(artifactType ArtifactType, version string) Artifact {
	return Artifact{
		Type:     artifactType,
		Version:  version,
		Arch:     runtime.GOARCH,
		Platform: runtime.GOOS,
	}
}

// GetDownloadURL returns the asset service URL for this artifact
func (a Artifact) GetDownloadURL() string {
	baseURL := "https://api.miren.cloud/assets/release/miren"

	// Binary artifacts (just the miren binary) - available for all platforms as .zip
	if a.Type == ArtifactTypeBinary {
		filename := fmt.Sprintf("miren-%s-%s.zip", a.Platform, a.Arch)
		fullURL, _ := url.JoinPath(baseURL, a.Version, filename)
		return fullURL
	}
	// Base artifacts (full release package) - only available for Linux as .tar.gz
	if a.Type == ArtifactTypeBase || a.Type == ArtifactTypeRelease {
		if a.Platform == "linux" {
			filename := fmt.Sprintf("miren-base-%s-%s.tar.gz", a.Platform, a.Arch)
			fullURL, _ := url.JoinPath(baseURL, a.Version, filename)
			return fullURL
		}
		// Non-Linux platforms should use binary artifacts
		filename := fmt.Sprintf("miren-%s-%s.zip", a.Platform, a.Arch)
		fullURL, _ := url.JoinPath(baseURL, a.Version, filename)
		return fullURL
	}
	// Fallback (shouldn't reach here)
	filename := fmt.Sprintf("miren-%s-%s-%s.tar.gz", a.Type, a.Platform, a.Arch)
	fullURL, _ := url.JoinPath(baseURL, a.Version, filename)
	return fullURL
}

// GetChecksumURL returns the checksum URL for this artifact
func (a Artifact) GetChecksumURL() string {
	return a.GetDownloadURL() + ".sha256"
}

// DownloadedArtifact represents a successfully downloaded artifact
type DownloadedArtifact struct {
	Artifact Artifact
	Path     string
	Checksum string
	Size     int64
}
