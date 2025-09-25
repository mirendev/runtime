package release

import (
	"encoding/json"
	"time"
)

// ArtifactInfo contains information about a release artifact
type ArtifactInfo struct {
	Name     string `json:"name"`               // Artifact filename
	SHA256   string `json:"sha256"`             // SHA256 checksum
	Size     int64  `json:"size,omitempty"`     // File size in bytes (optional)
	Platform string `json:"platform,omitempty"` // Target platform (optional)
	Arch     string `json:"arch,omitempty"`     // Target architecture (optional)
}

// Metadata contains version metadata for a release
type Metadata struct {
	Version   string         `json:"version"`    // Version string (e.g., "main:abc123")
	Commit    string         `json:"commit"`     // Full commit SHA
	Branch    string         `json:"branch"`     // Branch name
	BuildDate time.Time      `json:"build_date"` // Build timestamp
	Artifacts []ArtifactInfo `json:"artifacts"`  // List of available artifacts
}

// ParseMetadata parses JSON metadata
func ParseMetadata(data []byte) (*Metadata, error) {
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ToJSON converts metadata to JSON
func (m *Metadata) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// GetVersionString returns the version string for display
func (m *Metadata) GetVersionString() string {
	if m.Version != "" {
		return m.Version
	}
	// Fallback to branch:commit format
	if m.Branch != "" && m.Commit != "" {
		shortCommit := m.Commit
		if len(shortCommit) > 7 {
			shortCommit = shortCommit[:7]
		}
		return m.Branch + ":" + shortCommit
	}
	return "unknown"
}

// FindArtifact finds an artifact by name
func (m *Metadata) FindArtifact(name string) (*ArtifactInfo, bool) {
	for i := range m.Artifacts {
		if m.Artifacts[i].Name == name {
			return &m.Artifacts[i], true
		}
	}
	return nil, false
}

// HasArtifact checks if an artifact exists in the metadata
func (m *Metadata) HasArtifact(name string) bool {
	_, found := m.FindArtifact(name)
	return found
}

// GetArtifactChecksum returns the SHA256 checksum for an artifact
func (m *Metadata) GetArtifactChecksum(name string) (string, bool) {
	artifact, found := m.FindArtifact(name)
	if !found {
		return "", false
	}
	return artifact.SHA256, true
}
