package release

import (
	"testing"
	"time"
)

func TestParseMetadata(t *testing.T) {
	jsonData := `{
		"version": "main:abc123",
		"commit": "abc123def456789",
		"branch": "main",
		"build_date": "2025-09-25T20:00:00Z",
		"artifacts": [
			{"name": "miren-base-linux-amd64.tar.gz", "sha256": "sha256hash1", "size": 1024},
			{"name": "miren-base-linux-arm64.tar.gz", "sha256": "sha256hash2", "size": 2048},
			{"name": "miren-linux-amd64.zip", "sha256": "sha256hash3", "platform": "linux", "arch": "amd64"}
		]
	}`

	metadata, err := ParseMetadata([]byte(jsonData))
	if err != nil {
		t.Fatalf("ParseMetadata failed: %v", err)
	}

	if metadata.Version != "main:abc123" {
		t.Errorf("Version mismatch: got %s, want main:abc123", metadata.Version)
	}

	if metadata.Branch != "main" {
		t.Errorf("Branch mismatch: got %s, want main", metadata.Branch)
	}

	if len(metadata.Artifacts) != 3 {
		t.Errorf("Artifacts count mismatch: got %d, want 3", len(metadata.Artifacts))
	}

	// Test FindArtifact
	artifact, found := metadata.FindArtifact("miren-base-linux-amd64.tar.gz")
	if !found {
		t.Error("FindArtifact should find existing artifact")
	}
	if artifact.SHA256 != "sha256hash1" {
		t.Errorf("Artifact SHA256 mismatch: got %s, want sha256hash1", artifact.SHA256)
	}
	if artifact.Size != 1024 {
		t.Errorf("Artifact Size mismatch: got %d, want 1024", artifact.Size)
	}

	// Test HasArtifact
	if !metadata.HasArtifact("miren-base-linux-amd64.tar.gz") {
		t.Error("HasArtifact should return true for existing artifact")
	}

	if metadata.HasArtifact("nonexistent.tar.gz") {
		t.Error("HasArtifact should return false for non-existing artifact")
	}

	// Test GetArtifactChecksum
	checksum, exists := metadata.GetArtifactChecksum("miren-base-linux-amd64.tar.gz")
	if !exists {
		t.Error("GetArtifactChecksum should find existing artifact")
	}
	if checksum != "sha256hash1" {
		t.Errorf("Checksum mismatch: got %s, want sha256hash1", checksum)
	}

	_, exists = metadata.GetArtifactChecksum("nonexistent.tar.gz")
	if exists {
		t.Error("GetArtifactChecksum should not find non-existing artifact")
	}
}

func TestGetVersionString(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		want     string
	}{
		{
			name: "version field present",
			metadata: Metadata{
				Version: "v1.2.3",
			},
			want: "v1.2.3",
		},
		{
			name: "fallback to branch:commit",
			metadata: Metadata{
				Branch: "main",
				Commit: "abc123def456789",
			},
			want: "main:abc123d",
		},
		{
			name:     "empty metadata",
			metadata: Metadata{},
			want:     "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.metadata.GetVersionString()
			if got != tt.want {
				t.Errorf("GetVersionString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadataToJSON(t *testing.T) {
	metadata := Metadata{
		Version:   "main:abc123",
		Commit:    "abc123def456789",
		Branch:    "main",
		BuildDate: time.Date(2025, 9, 25, 20, 0, 0, 0, time.UTC),
		Artifacts: []ArtifactInfo{
			{Name: "artifact1.tar.gz", SHA256: "checksum1", Size: 1024},
			{Name: "artifact2.tar.gz", SHA256: "checksum2", Size: 2048},
		},
	}

	jsonData, err := metadata.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Parse it back to verify round-trip
	parsed, err := ParseMetadata(jsonData)
	if err != nil {
		t.Fatalf("ParseMetadata failed on ToJSON output: %v", err)
	}

	if parsed.Version != metadata.Version {
		t.Errorf("Round-trip version mismatch: got %s, want %s", parsed.Version, metadata.Version)
	}

	if len(parsed.Artifacts) != len(metadata.Artifacts) {
		t.Errorf("Round-trip artifacts count mismatch: got %d, want %d",
			len(parsed.Artifacts), len(metadata.Artifacts))
	}

	// Verify artifact details preserved
	for i, artifact := range metadata.Artifacts {
		if parsed.Artifacts[i].Name != artifact.Name {
			t.Errorf("Round-trip artifact[%d] name mismatch: got %s, want %s",
				i, parsed.Artifacts[i].Name, artifact.Name)
		}
		if parsed.Artifacts[i].SHA256 != artifact.SHA256 {
			t.Errorf("Round-trip artifact[%d] SHA256 mismatch: got %s, want %s",
				i, parsed.Artifacts[i].SHA256, artifact.SHA256)
		}
	}
}
