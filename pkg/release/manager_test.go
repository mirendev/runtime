package release

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeDownloader struct {
	metadata  map[string]*Metadata
	mismatch  bool
	checksums []string
}

func (d *fakeDownloader) Download(_ context.Context, a Artifact, opts DownloadOptions) (*DownloadedArtifact, error) {
	d.checksums = append(d.checksums, opts.ExpectedChecksum)
	if d.mismatch {
		return nil, fmt.Errorf("checksum verification failed: %w: expected %s, got other", ErrChecksumMismatch, opts.ExpectedChecksum)
	}
	return &DownloadedArtifact{Artifact: a}, nil
}

func (d *fakeDownloader) GetLatestVersion(context.Context, ArtifactType) (string, error) {
	return "main", nil
}

func (d *fakeDownloader) GetVersionMetadata(_ context.Context, v string) (*Metadata, error) {
	m, ok := d.metadata[v]
	if !ok {
		return nil, fmt.Errorf("metadata not found (HTTP 404)")
	}
	return m, nil
}

type fakeInstaller struct{ installs int }

func (i *fakeInstaller) Install(context.Context, *DownloadedArtifact) error { i.installs++; return nil }
func (i *fakeInstaller) Backup(context.Context) error                       { return nil }
func (i *fakeInstaller) Rollback(context.Context) error                     { return nil }
func (i *fakeInstaller) GetCurrentVersion(context.Context) (VersionInfo, error) {
	return VersionInfo{}, nil
}
func (i *fakeInstaller) HasBackup() bool { return false }

func testArtifact() Artifact {
	return Artifact{Type: ArtifactTypeBinary, Version: "main", Platform: "linux", Arch: "amd64"}
}

func TestUpgradeArtifactChecksAgainstManifest(t *testing.T) {
	dl := &fakeDownloader{metadata: map[string]*Metadata{
		"main": {Version: "main:b2975c1", Artifacts: []ArtifactInfo{
			{Name: "miren-linux-amd64.tar.gz", SHA256: "00e42230"},
		}},
	}}
	inst := &fakeInstaller{}
	mgr := NewManager(DefaultManagerOptions()).WithDownloader(dl).WithInstaller(inst)

	require.NoError(t, mgr.UpgradeArtifact(context.Background(), testArtifact()))
	require.Equal(t, []string{"00e42230"}, dl.checksums)
	require.Equal(t, 1, inst.installs)
}

// Without a manifest (a local asset server, say) the downloader is left to
// fetch .sha256 itself, as before.
func TestUpgradeArtifactWithoutManifestFallsBackToChecksumFile(t *testing.T) {
	dl := &fakeDownloader{}
	mgr := NewManager(DefaultManagerOptions()).WithDownloader(dl).WithInstaller(&fakeInstaller{})

	require.NoError(t, mgr.UpgradeArtifact(context.Background(), testArtifact()))
	require.Equal(t, []string{""}, dl.checksums)
}

func TestUpgradeArtifactExplainsChecksumMismatch(t *testing.T) {
	dl := &fakeDownloader{mismatch: true}
	inst := &fakeInstaller{}
	mgr := NewManager(DefaultManagerOptions()).WithDownloader(dl).WithInstaller(inst)

	err := mgr.UpgradeArtifact(context.Background(), testArtifact())
	require.ErrorIs(t, err, ErrChecksumMismatch)
	require.Contains(t, err.Error(), "try again in a minute")
	require.Equal(t, 0, inst.installs)
}
