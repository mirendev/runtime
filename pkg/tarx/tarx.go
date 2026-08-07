package tarx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/tonistiigi/fsutil"
)

// FileManifest represents a file's metadata for delta upload comparison.
type FileManifest struct {
	Path string
	Hash string
	Size int64
	Mode int32
}

// parseGitignoreFile reads a .gitignore file and returns its parsed patterns
// scoped to the given domain (path components from the walk root). A missing
// file is not an error and returns (nil, nil); other read failures (e.g.
// permission denied) propagate up so we don't silently treat them as "no
// patterns" and ship a tar that should have been filtered.
func parseGitignoreFile(path string, domain []string) ([]gitignore.Pattern, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var patterns []gitignore.Pattern
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, gitignore.ParsePattern(line, domain))
	}
	return patterns, nil
}

// parseStringPatterns parses a list of raw gitignore-style pattern strings
// (e.g. user-supplied --include patterns) into gitignore.Patterns rooted at
// the walk root.
func parseStringPatterns(rawPatterns []string) []gitignore.Pattern {
	if len(rawPatterns) == 0 {
		return nil
	}
	patterns := make([]gitignore.Pattern, 0, len(rawPatterns))
	for _, p := range rawPatterns {
		patterns = append(patterns, gitignore.ParsePattern(p, nil))
	}
	return patterns
}

// pathSegments converts an OS-relative path into the []string component form
// that gitignore.Matcher expects.
func pathSegments(rp string) []string {
	rp = filepath.ToSlash(rp)
	if rp == "" || rp == "." {
		return nil
	}
	return strings.Split(rp, "/")
}

// ValidatePattern checks that a user-supplied gitignore-style pattern parses
// successfully.
func ValidatePattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("invalid pattern syntax: empty pattern")
	}
	gitignore.ParsePattern(pattern, nil)
	return nil
}

func MakeTar(dir string, includePatterns []string, uncompressedBytes *atomic.Int64) (io.ReadCloser, error) {
	return makeTarWithFilter(dir, includePatterns, func(string) bool { return true }, uncompressedBytes)
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

// ComputeManifest walks a directory using the same gitignore/include logic as MakeTar
// and returns a manifest of all regular files with their SHA-256 hashes.
func ComputeManifest(dir string, includePatterns []string) ([]FileManifest, error) {
	ignorePatterns, err := parseGitignoreFile(filepath.Join(dir, ".gitignore"), nil)
	if err != nil {
		return nil, err
	}
	ignorePatterns = append(ignorePatterns, gitignore.ParsePattern(".git", nil))
	includes := parseStringPatterns(includePatterns)

	includesMatcher := gitignore.NewMatcher(includes)
	ignoreMatcher := gitignore.NewMatcher(ignorePatterns)

	var manifest []FileManifest

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if dir == path {
			return nil
		}

		rp, _ := filepath.Rel(dir, path)

		if filepath.Base(rp) == ".gitignore" {
			return nil
		}

		segs := pathSegments(rp)

		isIncluded := len(includes) > 0 && includesMatcher.Match(segs, info.IsDir())

		if !isIncluded {
			if ignoreMatcher.Match(segs, info.IsDir()) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if info.IsDir() {
			nestedGitignore := filepath.Join(path, ".gitignore")
			more, err := parseGitignoreFile(nestedGitignore, segs)
			if err != nil {
				return err
			}
			if more != nil {
				ignorePatterns = append(ignorePatterns, more...)
				ignoreMatcher = gitignore.NewMatcher(ignorePatterns)
			}
			return nil
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		hash, hashErr := hashFile(path)
		if hashErr != nil {
			return fmt.Errorf("hashing %s: %w", rp, hashErr)
		}

		manifest = append(manifest, FileManifest{
			Path: rp,
			Hash: hash,
			Size: info.Size(),
			Mode: int32(info.Mode().Perm()),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return manifest, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// MakeEmptyTar returns a valid empty gzipped tar archive.
func MakeEmptyTar() io.ReadCloser {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	tw.Close()
	gzw.Close()
	return io.NopCloser(&buf)
}

// MakeFilteredTar creates a gzipped tar like MakeTar but only includes files
// whose relative paths are in the onlyPaths set. Directory entries are included
// as needed to contain the requested files. If uncompressedBytes is non-nil,
// each file's uncompressed size is atomically added after it is written.
func MakeFilteredTar(dir string, includePatterns []string, onlyPaths map[string]bool, uncompressedBytes *atomic.Int64) (io.ReadCloser, error) {
	return makeTarWithFilter(dir, includePatterns, func(rp string) bool { return onlyPaths[rp] }, uncompressedBytes)
}

// makeTarWithFilter creates a gzipped tar of dir, applying gitignore/include logic,
// and only including regular files for which accept returns true. Directory entries
// are emitted lazily as needed to contain accepted files.
func makeTarWithFilter(dir string, includePatterns []string, accept func(string) bool, uncompressedBytes *atomic.Int64) (io.ReadCloser, error) {
	ignorePatterns, err := parseGitignoreFile(filepath.Join(dir, ".gitignore"), nil)
	if err != nil {
		return nil, err
	}
	ignorePatterns = append(ignorePatterns, gitignore.ParsePattern(".git", nil))
	includes := parseStringPatterns(includePatterns)

	includesMatcher := gitignore.NewMatcher(includes)
	ignoreMatcher := gitignore.NewMatcher(ignorePatterns)

	// io.Pipe (rather than os.Pipe) lets us surface walk errors to the
	// reader via CloseWithError instead of silently EOF-ing.
	r, w := io.Pipe()
	gzw := gzip.NewWriter(w)
	tw := tar.NewWriter(gzw)

	go func() {
		emittedDirs := make(map[string]bool)

		walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if dir == path {
				return nil
			}

			rp, _ := filepath.Rel(dir, path)

			if filepath.Base(rp) == ".gitignore" {
				return nil
			}

			segs := pathSegments(rp)

			isIncluded := len(includes) > 0 && includesMatcher.Match(segs, info.IsDir())

			if !isIncluded {
				if ignoreMatcher.Match(segs, info.IsDir()) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			if info.IsDir() {
				nestedGitignore := filepath.Join(path, ".gitignore")
				more, err := parseGitignoreFile(nestedGitignore, segs)
				if err != nil {
					return err
				}
				if more != nil {
					ignorePatterns = append(ignorePatterns, more...)
					ignoreMatcher = gitignore.NewMatcher(ignorePatterns)
				}
				return nil
			}

			if !accept(rp) {
				return nil
			}

			// Emit parent directories if not yet emitted
			parts := strings.Split(filepath.Dir(rp), string(filepath.Separator))
			for i := range parts {
				if parts[i] == "." {
					continue
				}
				dirPath := strings.Join(parts[:i+1], string(filepath.Separator))
				if emittedDirs[dirPath] {
					continue
				}
				emittedDirs[dirPath] = true

				dirInfo, dirErr := os.Stat(filepath.Join(dir, dirPath))
				if dirErr != nil {
					continue
				}
				dirHdr, hdrErr := tar.FileInfoHeader(dirInfo, "")
				if hdrErr != nil {
					continue
				}
				dirHdr.Name = dirPath
				if err := tw.WriteHeader(dirHdr); err != nil {
					return err
				}
			}

			hdr, hdrErr := tar.FileInfoHeader(info, "")
			if hdrErr != nil {
				return hdrErr
			}
			hdr.Name = rp

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				f, fErr := os.Open(path)
				if fErr != nil {
					return fErr
				}
				defer f.Close()
				if _, err := io.Copy(tw, &countingReader{r: f, counter: uncompressedBytes}); err != nil {
					return err
				}
			}

			return nil
		})

		// Close the tar/gzip writers before the pipe so their trailers
		// flush. Order matters: tw must close before gzw, gzw before w.
		// On walk error, propagate it through the pipe so the reader
		// gets the actual failure instead of a silent EOF.
		if cerr := tw.Close(); cerr != nil && walkErr == nil {
			walkErr = cerr
		}
		if cerr := gzw.Close(); cerr != nil && walkErr == nil {
			walkErr = cerr
		}
		if walkErr != nil {
			w.CloseWithError(walkErr)
			return
		}
		w.Close()
	}()

	return r, nil
}

// countingReader wraps an io.Reader and atomically adds bytes read to a counter.
type countingReader struct {
	r       io.Reader
	counter *atomic.Int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 && cr.counter != nil {
		cr.counter.Add(int64(n))
	}
	return n, err
}

// SafePath joins an untrusted archive path to root after verifying that the
// result remains within root.
//
// The containment check runs on the entry name rather than on the joined
// result. filepath.IsLocal rejects absolute paths and anything that climbs out
// via "..", and checking the name up front is the one shape CodeQL's zip-slip
// sanitizer model recognizes, so callers that extract through this helper stop
// tripping go/zipslip.
func SafePath(root, name string) (string, error) {
	clean := filepath.FromSlash(name)
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("archive path escapes destination directory: %q", name)
	}

	return filepath.Join(root, clean), nil
}

// SafeWritePath validates an archive path and rejects existing symlinks in the
// destination path so extraction cannot write through them.
func SafeWritePath(root, name string) (string, error) {
	rootInfo, err := os.Lstat(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("inspecting archive root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("archive root is a symlink: %q", root)
	}

	target, err := SafePath(root, name)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("resolving archive path %q: %w", name, err)
	}

	current := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("inspecting archive path %q: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("archive path traverses symlink: %q", name)
		}
	}

	return target, nil
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

		path, err := SafeWritePath(dir, th.Name)
		if err != nil {
			return nil, err
		}
		if th.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
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
