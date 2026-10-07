package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/pkg/tarx"
)

// Downloader handles artifact downloads from the asset service
type Downloader interface {
	Download(ctx context.Context, artifact Artifact, opts DownloadOptions) (*DownloadedArtifact, error)
	GetLatestVersion(ctx context.Context, artifactType ArtifactType) (string, error)
	GetVersionMetadata(ctx context.Context, version string) (*Metadata, error)
}

// DownloadOptions contains options for downloading artifacts
type DownloadOptions struct {
	// TargetDir is where to download the artifact
	TargetDir string
	// ProgressWriter receives progress updates (optional)
	ProgressWriter io.Writer
	// SkipChecksum skips checksum verification (not recommended)
	SkipChecksum bool
	// ExpectedChecksum allows providing a known checksum from metadata
	ExpectedChecksum string
}

// assetDownloader implements Downloader using the Miren asset service
type assetDownloader struct {
	httpClient *http.Client
	baseURL    string
}

// NewDownloader creates a new asset service downloader
func NewDownloader() Downloader {
	return &assetDownloader{
		httpClient: &http.Client{
			Timeout: 30 * time.Minute, // Allow long downloads for large artifacts
		},
		baseURL: AssetBaseURL(),
	}
}

// Download downloads an artifact from the asset service
func (d *assetDownloader) Download(ctx context.Context, artifact Artifact, opts DownloadOptions) (*DownloadedArtifact, error) {
	// Create temp directory for download
	tmpDir, err := os.MkdirTemp(opts.TargetDir, "miren-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Determine expected checksum
	var expectedChecksum string
	if !opts.SkipChecksum {
		if opts.ExpectedChecksum != "" {
			// Use provided checksum from metadata
			expectedChecksum = opts.ExpectedChecksum
		} else {
			// Download checksum file
			checksum, err := d.downloadChecksum(ctx, artifact)
			if err != nil {
				return nil, fmt.Errorf("failed to download checksum from %s: %w", artifact.GetChecksumURL(), err)
			}
			expectedChecksum = checksum
		}
	}

	// Download artifact
	downloadURL := artifact.GetDownloadURL()
	archivePath := filepath.Join(tmpDir, "artifact.tar.gz")

	size, err := d.downloadFile(ctx, downloadURL, archivePath, opts.ProgressWriter)
	if err != nil {
		return nil, fmt.Errorf("failed to download artifact: %w", err)
	}

	// Verify checksum
	if !opts.SkipChecksum {
		if err := d.verifyChecksum(archivePath, expectedChecksum); err != nil {
			return nil, fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	staged, err := d.extractTarGz(archivePath, opts.TargetDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	return &DownloadedArtifact{
		Artifact:  artifact,
		Path:      staged.miren,
		BundleDir: staged.dir,
		Bundled:   staged.bundled,
		Checksum:  expectedChecksum,
		Size:      size,
	}, nil
}

// GetLatestVersion returns the latest available version
func (d *assetDownloader) GetLatestVersion(ctx context.Context, artifactType ArtifactType) (string, error) {
	// Fetch version metadata file
	metadataURL := fmt.Sprintf("%s/main/version.json", d.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", metadataURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		// Fall back to "main" if metadata doesn't exist yet
		return "main", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fall back to "main" if metadata doesn't exist yet
		return "main", nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read metadata: %w", err)
	}

	metadata, err := ParseMetadata(data)
	if err != nil {
		// Fall back to "main" if metadata is invalid
		return "main", nil
	}

	return metadata.GetVersionString(), nil
}

// GetVersionMetadata returns detailed metadata for a specific version
func (d *assetDownloader) GetVersionMetadata(ctx context.Context, version string) (*Metadata, error) {
	// Default to "main" if no version specified
	if version == "" {
		version = "main"
	}

	metadataURL, _ := url.JoinPath(d.baseURL, version, "version.json")

	req, err := http.NewRequestWithContext(ctx, "GET", metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata not found (HTTP %d)", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	return ParseMetadata(data)
}

// downloadChecksum downloads the checksum file for an artifact
func (d *assetDownloader) downloadChecksum(ctx context.Context, artifact Artifact) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", artifact.GetChecksumURL(), nil)
	if err != nil {
		return "", err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	checksumData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Checksum file format: "hash  filename"
	parts := strings.Fields(string(checksumData))
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid checksum format")
	}

	return parts[0], nil
}

// downloadFile downloads a file from URL to path
func (d *assetDownloader) downloadFile(ctx context.Context, url, path string, progressWriter io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	out, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	// Set up progress tracking if a progress writer is provided
	var reader io.Reader = resp.Body
	if progressWriter != nil {
		if pw, ok := progressWriter.(interface{ SetTotal(int64) }); ok {
			pw.SetTotal(resp.ContentLength)
		}
		if pc, ok := progressWriter.(io.Closer); ok {
			defer pc.Close()
		}
		reader = io.TeeReader(resp.Body, progressWriter)
	}

	written, err := io.Copy(out, reader)
	if err != nil {
		return 0, err
	}

	return written, nil
}

// verifyChecksum verifies the SHA256 checksum of a file
func (d *assetDownloader) verifyChecksum(filePath, expectedChecksum string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// stagedArchive is an archive extracted under dir, ready for the installer.
type stagedArchive struct {
	dir     string
	miren   string
	bundled []string
}

// extractTarGz stages every regular file in the archive under a fresh
// directory in targetDir. The base package carries containerd, runc and the
// shim beside miren, and an upgrade that kept only miren would leave the
// host on the containerd it was first installed with. Symlinks and hard
// links are dropped, and the archive must contain a miren binary.
func (d *assetDownloader) extractTarGz(tarPath, targetDir string) (*stagedArchive, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	stageDir, err := os.MkdirTemp(targetDir, "miren-stage-*")
	if err != nil {
		return nil, err
	}
	staged := &stagedArchive{dir: stageDir}
	defer func() {
		if staged.miren == "" {
			os.RemoveAll(stageDir)
		}
	}()

	var mirenPath string
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Entries naming the extraction root itself carry nothing to write.
		if header.Name == "" || filepath.Clean(filepath.FromSlash(header.Name)) == "." {
			continue
		}

		targetPath, err := tarx.SafeWritePath(stageDir, header.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid tar entry %q: %w", header.Name, err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return nil, err
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return nil, err
			}
			outFile.Close()
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)); err != nil {
				return nil, err
			}

			rel, err := filepath.Rel(stageDir, targetPath)
			if err != nil {
				return nil, err
			}
			if rel == "miren" {
				mirenPath = targetPath
			} else {
				staged.bundled = append(staged.bundled, rel)
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Skip symlinks and hard links for security
			continue
		}
	}

	if mirenPath == "" {
		return nil, fmt.Errorf("miren binary not found in archive")
	}
	staged.miren = mirenPath
	return staged, nil
}
