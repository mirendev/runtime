package release

import (
	"runtime"
	"testing"
)

func TestNewArtifact(t *testing.T) {
	tests := []struct {
		name         string
		artifactType ArtifactType
		version      string
		want         Artifact
	}{
		{
			name:         "base artifact with main version",
			artifactType: ArtifactTypeBase,
			version:      "main",
			want: Artifact{
				Type:     ArtifactTypeBase,
				Version:  "main",
				Arch:     runtime.GOARCH,
				Platform: runtime.GOOS,
			},
		},
		{
			name:         "release artifact with version tag",
			artifactType: ArtifactTypeRelease,
			version:      "v1.2.3",
			want: Artifact{
				Type:     ArtifactTypeRelease,
				Version:  "v1.2.3",
				Arch:     runtime.GOARCH,
				Platform: runtime.GOOS,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewArtifact(tt.artifactType, tt.version)
			if got.Type != tt.want.Type {
				t.Errorf("NewArtifact() Type = %v, want %v", got.Type, tt.want.Type)
			}
			if got.Version != tt.want.Version {
				t.Errorf("NewArtifact() Version = %v, want %v", got.Version, tt.want.Version)
			}
			if got.Arch != tt.want.Arch {
				t.Errorf("NewArtifact() Arch = %v, want %v", got.Arch, tt.want.Arch)
			}
			if got.Platform != tt.want.Platform {
				t.Errorf("NewArtifact() Platform = %v, want %v", got.Platform, tt.want.Platform)
			}
		})
	}
}

func TestArtifact_GetDownloadURL(t *testing.T) {
	tests := []struct {
		name     string
		artifact Artifact
		want     string
	}{
		{
			name: "base artifact linux amd64",
			artifact: Artifact{
				Type:     ArtifactTypeBase,
				Version:  "main",
				Arch:     "amd64",
				Platform: "linux",
			},
			want: "https://api.miren.cloud/assets/release/miren/main/miren-base-linux-amd64.tar.gz",
		},
		{
			name: "base artifact linux arm64",
			artifact: Artifact{
				Type:     ArtifactTypeBase,
				Version:  "v1.2.3",
				Arch:     "arm64",
				Platform: "linux",
			},
			want: "https://api.miren.cloud/assets/release/miren/v1.2.3/miren-base-linux-arm64.tar.gz",
		},
		{
			name: "release artifact non-linux fallback to zip",
			artifact: Artifact{
				Type:     ArtifactTypeRelease,
				Version:  "main",
				Arch:     "amd64",
				Platform: "darwin",
			},
			want: "https://api.miren.cloud/assets/release/miren/main/miren-darwin-amd64.zip",
		},
		{
			name: "binary artifact linux",
			artifact: Artifact{
				Type:     ArtifactTypeBinary,
				Version:  "main",
				Arch:     "amd64",
				Platform: "linux",
			},
			want: "https://api.miren.cloud/assets/release/miren/main/miren-linux-amd64.zip",
		},
		{
			name: "binary artifact darwin",
			artifact: Artifact{
				Type:     ArtifactTypeBinary,
				Version:  "v1.0.0",
				Arch:     "arm64",
				Platform: "darwin",
			},
			want: "https://api.miren.cloud/assets/release/miren/v1.0.0/miren-darwin-arm64.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.artifact.GetDownloadURL()
			if got != tt.want {
				t.Errorf("Artifact.GetDownloadURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArtifact_GetChecksumURL(t *testing.T) {
	artifact := Artifact{
		Type:     ArtifactTypeBase,
		Version:  "main",
		Arch:     "amd64",
		Platform: "linux",
	}

	want := "https://api.miren.cloud/assets/release/miren/main/miren-base-linux-amd64.tar.gz.sha256"
	got := artifact.GetChecksumURL()

	if got != want {
		t.Errorf("Artifact.GetChecksumURL() = %v, want %v", got, want)
	}
}
