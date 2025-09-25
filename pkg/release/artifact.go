package release

import (
	"fmt"
	"runtime"
)

// ArtifactType represents the type of release artifact
type ArtifactType string

const (
	// ArtifactTypeBase contains just the miren binary
	ArtifactTypeBase ArtifactType = "base"
	// ArtifactTypeRelease contains miren plus supporting binaries
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
	// Currently only supporting base artifacts on Linux
	if a.Type == ArtifactTypeBase && a.Platform == "linux" {
		return fmt.Sprintf("https://api.miren.cloud/assets/release/miren/%s/miren-base-%s-%s.tar.gz",
			a.Version, a.Platform, a.Arch)
	}
	// Future: support release artifacts and other platforms
	return fmt.Sprintf("https://api.miren.cloud/assets/release/miren/%s/miren-%s-%s-%s.tar.gz",
		a.Version, a.Type, a.Platform, a.Arch)
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
