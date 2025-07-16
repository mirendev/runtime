package tarx

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shibumi/go-pathspec"
	"github.com/tonistiigi/fsutil"
)

func MakeTar(dir string, log *slog.Logger) (io.Reader, error) {
	// Use os.Pipe for its 64KB kernel buffer, plus our own buffering
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	// Load .gitignore patterns
	var gitignorePatterns []string
	gitignorePath := filepath.Join(dir, ".gitignore")
	if data, err := os.ReadFile(gitignorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				gitignorePatterns = append(gitignorePatterns, line)
				// If pattern ends with /, also add pattern without / to match the directory itself
				if strings.HasSuffix(line, "/") {
					gitignorePatterns = append(gitignorePatterns, strings.TrimSuffix(line, "/"))
				}
			}
		}
	}

	gitignorePatterns = append(gitignorePatterns, ".git") // Always ignore .git directory

	// Check if vendor directory exists
	vendorPath := filepath.Join(dir, "vendor")
	vendorExists := false
	if info, err := os.Stat(vendorPath); err == nil && info.IsDir() {
		vendorExists = true
		log.Info("vendor directory detected, will include despite gitignore")
	}

	go func() {
		// Add a large buffer to batch small gzip writes into larger chunks
		// This prevents the 240-byte problem where gzip writes small chunks
		bw := bufio.NewWriterSize(pw, 64*1024) // 64KB buffer
		gzw := gzip.NewWriter(bw)
		tw := tar.NewWriter(gzw)

		// Track progress
		fileCount := 0
		totalBytes := int64(0)

		var walkErr error

		defer func() {
			if err := tw.Close(); err != nil && walkErr == nil {
				log.Error("error closing tar writer", "error", err)
			}
			if err := gzw.Close(); err != nil && walkErr == nil {
				log.Error("error closing gzip writer", "error", err)
			}
			// Flush the buffer before closing the pipe
			if err := bw.Flush(); err != nil && walkErr == nil {
				log.Error("error flushing buffer", "error", err)
			}
			// Always close the pipe writer
			if err := pw.Close(); err != nil && walkErr == nil {
				log.Error("error closing pipe writer", "error", err)
			}
			log.Info("tar streaming completed", "files", fileCount, "totalBytes", totalBytes, "error", walkErr)
		}()

		walkErr = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if dir == path {
				return nil
			}

			rp, _ := filepath.Rel(dir, path)

			// Skip .gitignore file itself
			if rp == ".gitignore" {
				return nil
			}

			// Check if this is under vendor/ directory
			isVendor := vendorExists && (rp == "vendor" || strings.HasPrefix(rp, "vendor/"))

			// If not vendor, check gitignore BEFORE writing header
			if !isVendor && len(gitignorePatterns) > 0 {
				if ignore, err := pathspec.GitIgnore(gitignorePatterns, rp); err == nil && ignore {
					log.Debug("ignoring file per gitignore", "path", rp)
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			// Log vendor processing
			if isVendor {
				log.Debug("including vendor path", "path", rp, "isDir", info.IsDir())
			}

			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			hdr.Name = rp

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer func() {
					if err := f.Close(); err != nil {
						log.Warn("failed to close file", "path", path, "error", err)
					}
				}()

				// Always use io.Copy which handles buffering internally
				written, err := io.Copy(tw, f)
				if err != nil {
					log.Error("failed to copy file to tar", "path", rp, "error", err)
					return err
				}

				fileCount++
				totalBytes += written
				if fileCount%100 == 0 && fileCount > 0 {
					log.Debug("tar creation progress", "files", fileCount, "totalBytes", totalBytes)
				}
			}

			return nil
		})

		if walkErr != nil {
			log.Error("error during tar creation", "error", walkErr)
		}
	}()

	return pr, nil
}

func TarToMap(r io.Reader) (map[string][]byte, error) {
	tr := tar.NewReader(r)

	m := make(map[string][]byte)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			break
		}

		if th == nil {
			break
		}

		if th.Typeflag != tar.TypeReg {
			continue
		}

		buf := make([]byte, th.Size)
		n, err := io.ReadFull(tr, buf)
		if n == 0 && err != nil && err != io.EOF {
			return nil, err
		}

		m[th.Name] = buf[:n]
	}

	return m, nil
}

func TarFS(r io.Reader, dir string, log *slog.Logger) (fsutil.FS, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "tarx.TarFS")
	log.Info("starting tar extraction", "dir", dir)

	startTime := time.Now()
	fileCount := 0
	totalBytes := int64(0)

	gzr, err := gzip.NewReader(r)
	if err != nil {
		log.Error("failed to create gzip reader", "error", err)
		return nil, err
	}

	tr := tar.NewReader(gzr)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			log.Info("tar extraction complete",
				"files", fileCount,
				"totalBytes", totalBytes,
				"duration", time.Since(startTime))
			break
		}
		if err != nil {
			log.Error("tar reader error",
				"error", err,
				"filesProcessed", fileCount)
			return nil, err
		}

		fileCount++
		path := filepath.Join(dir, th.Name)

		if fileCount%500 == 0 && fileCount > 0 {
			log.Debug("extraction progress",
				"files", fileCount,
				"totalBytes", totalBytes,
				"elapsed", time.Since(startTime))
		}

		if th.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path, 0755); err != nil {
				log.Error("failed to create directory",
					"path", path,
					"error", err)
				return nil, err
			}
		}

		if th.Typeflag == tar.TypeReg {
			f, err := os.Create(path)
			if err != nil {
				log.Error("failed to create file",
					"path", path,
					"error", err)
				return nil, err
			}

			n, err := io.Copy(f, tr)
			if err != nil {
				log.Error("failed to copy file data",
					"path", path,
					"error", err,
					"bytesCopied", n)
				_ = f.Close()
				return nil, err
			}

			totalBytes += n

			f.Chmod(os.FileMode(th.FileInfo().Mode()) & os.ModePerm)
			if err := f.Close(); err != nil {
				log.Warn("failed to close file",
					"path", path,
					"error", err)
			}
		}
	}

	return fsutil.NewFS(dir)
}
