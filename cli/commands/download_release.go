package commands

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"miren.dev/runtime/pkg/tarx"
	"miren.dev/runtime/version"
)

// DownloadReleaseOptions contains options for downloading a release
type DownloadReleaseOptions struct {
	Branch string
	Global bool
	Force  bool
	Output string
}

// PerformDownloadRelease performs the actual download with the given options
func PerformDownloadRelease(ctx *Context, opts DownloadReleaseOptions) error {
	// Determine the target architecture
	arch := runtime.GOARCH

	// Construct download URLs
	baseURL := fmt.Sprintf("https://api.miren.cloud/assets/release/miren/%s/miren-base-linux-%s.tar.gz", opts.Branch, arch)
	shaURL := baseURL + ".sha256"

	// Determine release directory
	var releaseDir string
	if opts.Output != "" {
		releaseDir = opts.Output
	} else if opts.Global {
		releaseDir = "/var/lib/miren/release"
	} else {
		// Use user's home directory, respecting SUDO_USER
		var homeDir string
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			// Running under sudo, get the original user's home
			u, err := user.Lookup(sudoUser)
			if err == nil {
				homeDir = u.HomeDir
			} else {
				// Fallback to HOME env var
				homeDir = os.Getenv("HOME")
			}
		} else {
			// Not running under sudo, use current user's home
			homeDir = os.Getenv("HOME")
			if homeDir == "" {
				if u, err := user.Current(); err == nil {
					homeDir = u.HomeDir
				}
			}
		}
		releaseDir = filepath.Join(homeDir, ".miren", "release")
	}

	// Check if release directory exists
	if _, err := os.Lstat(releaseDir); err == nil {
		if !opts.Force {
			return fmt.Errorf("release directory already exists at %s (use -f to force)", releaseDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect release directory: %w", err)
	}

	releaseParent := filepath.Dir(filepath.Clean(releaseDir))
	if err := os.MkdirAll(releaseParent, 0755); err != nil {
		return fmt.Errorf("failed to create release parent directory: %w", err)
	}

	ctx.Log.Info("downloading release", "branch", opts.Branch, "arch", arch, "url", baseURL)

	// Create temporary directory for downloads
	tempDir, err := os.MkdirTemp("", "miren-release-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Download SHA256 checksum
	ctx.Log.Info("downloading checksum", "url", shaURL)
	shaPath := filepath.Join(tempDir, "release.tar.gz.sha256")
	if err := downloadFile(shaPath, shaURL); err != nil {
		return fmt.Errorf("failed to download checksum from %s: %w", shaURL, err)
	}

	// Read expected checksum
	shaData, err := os.ReadFile(shaPath)
	if err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}
	expectedSum := strings.TrimSpace(strings.Split(string(shaData), " ")[0])

	// Download release tarball
	ctx.Log.Info("downloading release tarball")
	tarPath := filepath.Join(tempDir, "release.tar.gz")
	fmt.Printf("Downloading from %s\n", baseURL)
	if err := downloadFile(tarPath, baseURL); err != nil {
		return fmt.Errorf("failed to download release: %w", err)
	}

	// Verify checksum
	ctx.Log.Info("verifying checksum")
	actualSum, err := calculateSHA256(tarPath)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	if actualSum != expectedSum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSum, actualSum)
	}

	stagingDir, err := os.MkdirTemp(releaseParent, "."+filepath.Base(releaseDir)+"-staging-*")
	if err != nil {
		return fmt.Errorf("failed to create release staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	if err := os.Chmod(stagingDir, 0755); err != nil {
		return fmt.Errorf("failed to set release staging permissions: %w", err)
	}

	ctx.Log.Info("extracting release", "destination", stagingDir)
	if err := extractTarGz(tarPath, stagingDir); err != nil {
		return fmt.Errorf("failed to extract release: %w", err)
	}

	versionCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	out, err := releaseBinaryVersion(versionCtx, stagingDir)
	cancel()
	if err != nil {
		return err
	}

	backupDir, err := promoteReleaseDir(stagingDir, releaseDir, opts.Force)
	if err != nil {
		if backupDir != "" {
			return fmt.Errorf("failed to install release; previous release preserved at %s: %w", backupDir, err)
		}
		return fmt.Errorf("failed to install release: %w", err)
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			ctx.Log.Warn("failed to remove previous release backup", "path", backupDir, "error", err)
		}
	}

	ctx.Log.Info("release binary version", "version", strings.TrimSpace(string(out)))

	ctx.Log.Info("release downloaded successfully", "path", releaseDir)
	return nil
}

func releaseBinaryVersion(ctx context.Context, releaseDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, filepath.Join(releaseDir, "miren"), "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("failed to run release binary: %w (output: %s)", ctxErr, strings.TrimSpace(string(out)))
		}
		return nil, fmt.Errorf("failed to run release binary: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func promoteReleaseDir(stagingDir, releaseDir string, replaceExisting bool) (string, error) {
	if _, err := os.Lstat(releaseDir); os.IsNotExist(err) {
		return "", os.Rename(stagingDir, releaseDir)
	} else if err != nil {
		return "", err
	}
	if !replaceExisting {
		return "", fmt.Errorf("release directory already exists at %s", releaseDir)
	}

	backupDir, err := os.MkdirTemp(filepath.Dir(filepath.Clean(releaseDir)), "."+filepath.Base(releaseDir)+"-previous-*")
	if err != nil {
		return "", err
	}
	if err := os.Remove(backupDir); err != nil {
		return "", err
	}

	if err := os.Rename(releaseDir, backupDir); err != nil {
		return "", err
	}
	if err := os.Rename(stagingDir, releaseDir); err != nil {
		if rollbackErr := os.Rename(backupDir, releaseDir); rollbackErr != nil {
			return backupDir, fmt.Errorf("promoting staged release: %v (rollback failed: %w)", err, rollbackErr)
		}
		return "", fmt.Errorf("promoting staged release: %w", err)
	}

	return backupDir, nil
}

// DownloadRelease is the CLI command handler for downloading a release
func DownloadRelease(ctx *Context, opts struct {
	Branch string `short:"b" long:"branch" description:"Branch name to download"`
	Global bool   `short:"g" long:"global" description:"Install globally to /var/lib/miren/release"`
	Force  bool   `short:"f" long:"force" description:"Force download even if release directory exists"`
	Output string `short:"o" long:"output" description:"Custom output directory"`
}) error {
	if opts.Branch == "" {
		if br := version.Branch(); br != "" {
			opts.Branch = br
		} else {
			opts.Branch = "latest"
		}
	}

	return PerformDownloadRelease(ctx, DownloadReleaseOptions{
		Branch: opts.Branch,
		Global: opts.Global,
		Force:  opts.Force,
		Output: opts.Output,
	})
}

func downloadFile(filepath string, url string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Get the file size
	size := resp.ContentLength

	// Create progress reader
	reader := &progressReader{
		Reader: resp.Body,
		Total:  size,
		Start:  time.Now(),
	}

	// Write the body to file with progress
	_, err = io.Copy(out, reader)
	fmt.Println() // New line after progress
	return err
}

func calculateSHA256(filepath string) (string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractTarGz(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target, err := tarx.SafeWritePath(dest, header.Name)
		if err != nil {
			return fmt.Errorf("invalid tar entry %q: %w", header.Name, err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// Create the directories if necessary
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			// Create the file
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// Copy file contents
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("tar entry %q uses an unsupported link type", header.Name)
		}
	}

	return nil
}

// progressReader wraps an io.Reader and displays download progress
type progressReader struct {
	io.Reader
	Total     int64
	Current   int64
	Start     time.Time
	lastPrint time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.Current += int64(n)

	// Only update every 100ms to avoid flickering
	now := time.Now()
	if now.Sub(pr.lastPrint) >= 100*time.Millisecond || err == io.EOF {
		pr.printProgress()
		pr.lastPrint = now
	}

	return n, err
}

func (pr *progressReader) printProgress() {
	if pr.Total <= 0 {
		// Unknown size, just show bytes downloaded
		fmt.Printf("\rDownloading... %s", formatBytes(pr.Current))
		return
	}

	percent := float64(pr.Current) * 100 / float64(pr.Total)
	elapsed := time.Since(pr.Start)

	// Calculate speed
	speed := float64(pr.Current) / elapsed.Seconds()

	// Calculate ETA
	var eta time.Duration
	if pr.Current > 0 {
		totalTime := elapsed * time.Duration(pr.Total) / time.Duration(pr.Current)
		eta = totalTime - elapsed
	}

	// Progress bar
	width := 40
	filled := int(percent * float64(width) / 100)
	bar := strings.Repeat("=", filled) + strings.Repeat("-", width-filled)

	fmt.Printf("\r[%s] %.1f%% %s/%s @ %s/s ETA: %s         ",
		bar, percent,
		formatBytes(pr.Current), formatBytes(pr.Total),
		formatBytes(int64(speed)),
		formatDuration(eta))
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
