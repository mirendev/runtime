package tarx

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shibumi/go-pathspec"
	"github.com/tonistiigi/fsutil"
)

// ValidatePattern checks if a pattern is valid for use with pathspec.GitIgnore
func ValidatePattern(pattern string) error {
	// Test the pattern with a dummy path to ensure it's valid
	_, err := pathspec.GitIgnore([]string{pattern}, "test")
	if err != nil {
		return fmt.Errorf("invalid pattern syntax: %w", err)
	}
	return nil
}

func MakeTar(dir string, includePatterns []string) (io.Reader, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	gzw := gzip.NewWriter(w)
	tw := tar.NewWriter(gzw)

	// Load .gitignore patterns
	var gitignorePatterns []string
	gitignorePath := filepath.Join(dir, ".gitignore")
	if data, err := os.ReadFile(gitignorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				gitignorePatterns = append(gitignorePatterns, line)
			}
		}
	}

	gitignorePatterns = append(gitignorePatterns, ".git") // Always ignore .git directory

	go func() {
		defer w.Close()
		defer tw.Close()
		defer gzw.Close()

		// tar up dir and output it to tw
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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

			// Check if file matches include patterns first
			isIncluded := false
			if len(includePatterns) > 0 {
				// Try both with and without trailing slash for directories
				paths := []string{rp}
				if info.IsDir() {
					paths = append(paths, rp+"/")
				}

				for _, checkPath := range paths {
					// Use the same gitignore-style pattern matching as we use for excludes
					match, err := pathspec.GitIgnore(includePatterns, checkPath)
					if err != nil {
						return fmt.Errorf("invalid include pattern: %w", err)
					}
					if match {
						isIncluded = true
						break
					}
				}
			}

			// Skip gitignore check if file is explicitly included
			if !isIncluded && len(gitignorePatterns) > 0 {
				// Try both with and without trailing slash for directories
				paths := []string{rp}
				if info.IsDir() {
					paths = append(paths, rp+"/")
				}

				for _, checkPath := range paths {
					ignore, err := pathspec.GitIgnore(gitignorePatterns, checkPath)
					if err != nil {
						return fmt.Errorf("invalid gitignore pattern: %w", err)
					}
					if ignore {
						if info.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
				}
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
				defer f.Close()

				if _, err := io.Copy(tw, f); err != nil {
					return err
				}
			}

			return nil
		})
	}()

	return r, nil
}

func TarToMap(r io.Reader) (map[string][]byte, error) {
	tr := tar.NewReader(r)

	m := make(map[string][]byte)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
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

func TarFS(r io.Reader, dir string) (fsutil.FS, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(gzr)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		path := filepath.Join(dir, th.Name)
		if th.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path, 0755); err != nil {
				return nil, err
			}
		}

		if th.Typeflag == tar.TypeReg {
			f, err := os.Create(path)
			if err != nil {
				return nil, err
			}

			if _, err := io.Copy(f, tr); err != nil {
				return nil, err
			}

			f.Chmod(os.FileMode(th.FileInfo().Mode()) & os.ModePerm)
		}
	}

	return fsutil.NewFS(dir)
}
