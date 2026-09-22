package httpingress

import (
	"archive/tar"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/components/ocireg"
)

type staticFileServer interface {
	ServeFile(http.ResponseWriter, *http.Request, *core_v1alpha.AppVersion) (bool, error)
}

type archiveStaticFileServer struct {
	blobs *ocireg.BlobStore

	mu           sync.Mutex
	indexes      *lru.Cache[string, *staticArchiveIndex]
	indexEntries int
}

type staticArchiveIndex struct {
	files map[string]staticArchiveFile
	dirs  map[string]bool
}

const (
	maxStaticIndexes      = 256
	maxStaticIndexEntries = 100_000
)

type staticArchiveFile struct {
	offset  int64
	size    int64
	modTime time.Time
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func newArchiveStaticFileServer(dataPath string) staticFileServer {
	if dataPath == "" {
		return nil
	}
	indexes, _ := lru.New[string, *staticArchiveIndex](maxStaticIndexes)
	return &archiveStaticFileServer{
		blobs:   ocireg.NewBlobStore(dataPath),
		indexes: indexes,
	}
}

func (s *archiveStaticFileServer) ServeFile(w http.ResponseWriter, req *http.Request, version *core_v1alpha.AppVersion) (bool, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false, nil
	}
	if version.StaticArtifact == "" {
		return false, fmt.Errorf("application version has no static artifact")
	}

	index, err := s.index(version.StaticArtifact)
	if err != nil {
		return false, err
	}
	requestPath := strings.TrimPrefix(path.Clean("/"+req.URL.Path), "/")
	if requestPath == "." {
		requestPath = ""
	}
	if index.dirs[requestPath] {
		indexPath := path.Join(requestPath, "index.html")
		if _, ok := index.files[indexPath]; !ok {
			return false, nil
		}
		if !strings.HasSuffix(req.URL.Path, "/") {
			target := path.Base(req.URL.Path) + "/"
			if req.URL.RawQuery != "" {
				target += "?" + req.URL.RawQuery
			}
			http.Redirect(w, req, target, http.StatusMovedPermanently)
			return true, nil
		}
		requestPath = indexPath
	}

	entry, ok := index.files[requestPath]
	if !ok {
		return false, nil
	}
	archive, err := s.blobs.Open(version.StaticArtifact)
	if err != nil {
		return false, fmt.Errorf("opening static artifact: %w", err)
	}
	defer archive.Close()

	content := io.NewSectionReader(archive, entry.offset, entry.size)
	http.ServeContent(w, req, path.Base(requestPath), entry.modTime, content)
	return true, nil
}

func (s *archiveStaticFileServer) index(digest string) (*staticArchiveIndex, error) {
	s.mu.Lock()
	if index, ok := s.indexes.Get(digest); ok {
		s.mu.Unlock()
		return index, nil
	}
	s.mu.Unlock()

	archive, err := s.blobs.Open(digest)
	if err != nil {
		return nil, fmt.Errorf("opening static artifact: %w", err)
	}
	defer archive.Close()

	counter := &countingReader{r: archive}
	reader := tar.NewReader(counter)
	index := &staticArchiveIndex{
		files: make(map[string]staticArchiveFile),
		dirs:  map[string]bool{"": true},
	}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading static artifact: %w", err)
		}

		name := path.Clean(strings.TrimPrefix(header.Name, "./"))
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("static artifact contains invalid path %q", header.Name)
		}
		if header.Typeflag == tar.TypeDir {
			index.addDir(name)
			continue
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		index.files[name] = staticArchiveFile{
			offset:  counter.n,
			size:    header.Size,
			modTime: header.ModTime,
		}
		index.addDir(path.Dir(name))
	}

	entries := len(index.files) + len(index.dirs)
	if entries > maxStaticIndexEntries {
		return index, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.indexes.Get(digest); ok {
		return cached, nil
	}
	if s.indexes.Len() >= maxStaticIndexes {
		_, evicted, ok := s.indexes.RemoveOldest()
		if ok {
			s.indexEntries -= len(evicted.files) + len(evicted.dirs)
		}
	}
	s.indexes.Add(digest, index)
	s.indexEntries += entries
	for s.indexEntries > maxStaticIndexEntries && s.indexes.Len() > 1 {
		_, evicted, ok := s.indexes.RemoveOldest()
		if !ok {
			break
		}
		s.indexEntries -= len(evicted.files) + len(evicted.dirs)
	}
	return index, nil
}

func (i *staticArchiveIndex) addDir(name string) {
	for name != "." && name != "/" {
		i.dirs[name] = true
		name = path.Dir(name)
	}
	i.dirs[""] = true
}
