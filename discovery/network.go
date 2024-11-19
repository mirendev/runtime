package discovery

import (
	"errors"
	"fmt"
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

func (h *LocalContainerEndpoint) readFile(w http.ResponseWriter, req *http.Request) error {
	h.Log.Debug("trying to serve file", "path", req.URL.Path)

	ctx := namespaces.WithNamespace(req.Context(), h.Namespace)

	cont, err := h.Client.LoadContainer(ctx, h.Id)
	if err != nil {
		return err
	}

	task, err := cont.Task(ctx, nil)
	if err != nil {
		return err
	}

	status, err := task.Status(ctx)
	if err != nil {
		return err
	}

	if status.Status != containerd.Running {
		return errors.New("task is not running")
	}

	if containsDotDot(req.URL.Path) {
		return errors.New("illegal path request")
	}

	path := filepath.Join(
		fmt.Sprintf("/proc/%d/root", task.Pid()),
		h.Dir, req.URL.Path)

	f, err := os.Open(path)
	if err != nil {
		return ErrNotFound
	}

	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return ErrNotFound
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

	h.HTTP.ServeHTTP(w, req)
}
