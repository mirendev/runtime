package discovery

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
)

type HTTPEndpoint struct {
	Host string
}

func (h *HTTPEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var rp httputil.ReverseProxy
	rp.Director = h.redirect
	rp.ServeHTTP(w, req)
}

func (h *HTTPEndpoint) redirect(req *http.Request) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(h.Host, "http://")
}

type LocalContainerEndpoint struct {
	Log       *slog.Logger
	HTTP      HTTPEndpoint
	Client    *containerd.Client
	Namespace string
	Dir       string `asm:"static_dir"`
	Id        string
}

const upperdirLabel = "containerd.io/snapshot/overlay.upperdir"

func (h *LocalContainerEndpoint) readFile(w http.ResponseWriter, req *http.Request) error {
	if containsDotDot(req.URL.Path) {
		return errors.New("illegal path request")
	}

	h.Log.Debug("trying to serve file", "path", req.URL.Path)

	ctx := namespaces.WithNamespace(req.Context(), h.Namespace)

	cont, err := h.Client.LoadContainer(ctx, h.Id)
	if err != nil {
		return err
	}

	ci, err := cont.Info(ctx)
	if err != nil {
		return err
	}

	si, err := h.Client.SnapshotService(ci.Snapshotter).Stat(ctx, ci.SnapshotKey)
	if err != nil {
		return err
	}

	p, err := h.Client.SnapshotService(ci.Snapshotter).Stat(ctx, si.Parent)
	if err != nil {
		return err
	}

	root, ok := p.Labels[upperdirLabel]
	if !ok {
		return os.ErrNotExist
	}

	path := filepath.Join(
		root,
		h.Dir, req.URL.Path)

	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	h.Log.Debug("serving file", "path", path)

	http.ServeContent(w, req, path, fi.ModTime(), f)

	return nil
}

func isSlashRune(r rune) bool { return r == '/' || r == '\\' }

func containsDotDot(v string) bool {
	if !strings.Contains(v, "..") {
		return false
	}
	for _, ent := range strings.FieldsFunc(v, isSlashRune) {
		if ent == ".." {
			return true
		}
	}
	return false
}

func (h *LocalContainerEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	err := h.readFile(w, req)
	if err == nil {
		return
	}

	h.Log.Debug("unable to serve static file", "error", err, "path", req.URL.Path)

	h.HTTP.ServeHTTP(w, req)
}
