package release

import (
	"encoding/json"
	"time"
)

// Metadata contains version metadata for a release
type Metadata struct {
	Version   string    `json:"version"`    // Version string (e.g., "main:abc123")
	Commit    string    `json:"commit"`     // Full commit SHA
	Branch    string    `json:"branch"`     // Branch name
	BuildDate time.Time `json:"build_date"` // Build timestamp
	Artifacts []string  `json:"artifacts"`  // List of available artifacts
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