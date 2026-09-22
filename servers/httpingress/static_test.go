package httpingress

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestServeStaticArtifact(t *testing.T) {
	dataPath := t.TempDir()
	var contents bytes.Buffer
	archive := tar.NewWriter(&contents)
	modTime := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		header tar.Header
		body   string
	}{
		{header: tar.Header{Name: "asset.txt", Mode: 0644, Size: 5, ModTime: modTime, Typeflag: tar.TypeReg}, body: "asset"},
		{header: tar.Header{Name: "docs/", Mode: 0755, ModTime: modTime, Typeflag: tar.TypeDir}},
		{header: tar.Header{Name: "docs/index.html", Mode: 0644, Size: 10, ModTime: modTime, Typeflag: tar.TypeReg}, body: "docs index"},
		{header: tar.Header{Name: "listing/", Mode: 0755, ModTime: modTime, Typeflag: tar.TypeDir}},
		{header: tar.Header{Name: "listing/file.txt", Mode: 0644, Size: 6, ModTime: modTime, Typeflag: tar.TypeReg}, body: "hidden"},
		{header: tar.Header{Name: "escape", Mode: 0777, Linkname: "/etc/passwd", ModTime: modTime, Typeflag: tar.TypeSymlink}},
	} {
		require.NoError(t, archive.WriteHeader(&entry.header))
		_, err := archive.Write([]byte(entry.body))
		require.NoError(t, err)
	}
	require.NoError(t, archive.Close())
	digest, _, err := ocireg.NewBlobStore(dataPath).Put(bytes.NewReader(contents.Bytes()))
	require.NoError(t, err)
	server := newArchiveStaticFileServer(dataPath)
	version := &core_v1alpha.AppVersion{StaticArtifact: digest}

	tests := []struct {
		name         string
		method       string
		path         string
		rangeValue   string
		wantServed   bool
		wantStatus   int
		wantBody     string
		wantLocation string
	}{
		{name: "file", method: http.MethodGet, path: "/asset.txt", wantServed: true, wantStatus: http.StatusOK, wantBody: "asset"},
		{name: "head", method: http.MethodHead, path: "/asset.txt", wantServed: true, wantStatus: http.StatusOK},
		{name: "range", method: http.MethodGet, path: "/asset.txt", rangeValue: "bytes=1-3", wantServed: true, wantStatus: http.StatusPartialContent, wantBody: "sse"},
		{name: "directory index", method: http.MethodGet, path: "/docs/", wantServed: true, wantStatus: http.StatusOK, wantBody: "docs index"},
		{name: "directory redirect", method: http.MethodGet, path: "/docs?ref=x", wantServed: true, wantStatus: http.StatusMovedPermanently, wantBody: "<a href=\"/docs/?ref=x\">Moved Permanently</a>.\n\n", wantLocation: "/docs/?ref=x"},
		{name: "no directory listing", method: http.MethodGet, path: "/listing/", wantServed: false, wantStatus: http.StatusOK},
		{name: "no redirect without index", method: http.MethodGet, path: "/listing", wantServed: false, wantStatus: http.StatusOK},
		{name: "missing", method: http.MethodGet, path: "/missing.txt", wantServed: false, wantStatus: http.StatusOK},
		{name: "symlink ignored", method: http.MethodGet, path: "/escape", wantServed: false, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "http://example.com"+tt.path, nil)
			if tt.rangeValue != "" {
				request.Header.Set("Range", tt.rangeValue)
			}
			served, err := server.ServeFile(recorder, request, version)
			require.NoError(t, err)
			assert.Equal(t, tt.wantServed, served)
			assert.Equal(t, tt.wantStatus, recorder.Code)
			assert.Equal(t, tt.wantBody, recorder.Body.String())
			assert.Equal(t, tt.wantLocation, recorder.Header().Get("Location"))
		})
	}
}

func TestStaticArchiveIndexCacheIsBounded(t *testing.T) {
	dataPath := t.TempDir()
	blobs := ocireg.NewBlobStore(dataPath)
	server := newArchiveStaticFileServer(dataPath).(*archiveStaticFileServer)

	for i := 0; i <= maxStaticIndexes; i++ {
		var contents bytes.Buffer
		archive := tar.NewWriter(&contents)
		body := fmt.Sprintf("file-%d", i)
		require.NoError(t, archive.WriteHeader(&tar.Header{
			Name:     "index.html",
			Mode:     0644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := archive.Write([]byte(body))
		require.NoError(t, err)
		require.NoError(t, archive.Close())
		digest, _, err := blobs.Put(bytes.NewReader(contents.Bytes()))
		require.NoError(t, err)
		_, err = server.index(digest)
		require.NoError(t, err)
	}

	assert.Equal(t, maxStaticIndexes, server.indexes.Len())
	assert.LessOrEqual(t, server.indexEntries, maxStaticIndexEntries)
}

type fakeStaticFiles struct {
	served bool
	err    error
	calls  int
}

func (f *fakeStaticFiles) ServeFile(http.ResponseWriter, *http.Request, *core_v1alpha.AppVersion) (bool, error) {
	f.calls++
	return f.served, f.err
}

type panicActivator struct{}

func (panicActivator) AcquireLease(context.Context, *core_v1alpha.AppVersion, string) (*activator.Lease, error) {
	panic("static-only request attempted to acquire a sandbox lease")
}
func (panicActivator) ReleaseLease(context.Context, *activator.Lease) error { return nil }
func (panicActivator) RenewLease(context.Context, *activator.Lease) (*activator.Lease, error) {
	return nil, nil
}
func (panicActivator) Invalidations() <-chan activator.SandboxInvalidation {
	return make(chan activator.SandboxInvalidation)
}
func (panicActivator) SetPoolCreator(activator.PoolCreator) {}

type recordingActivator struct {
	stubActivator
	acquired bool
}

func (a *recordingActivator) AcquireLease(context.Context, *core_v1alpha.AppVersion, string) (*activator.Lease, error) {
	a.acquired = true
	return nil, errors.New("no sandbox available")
}

func TestStaticOnlyRequestDoesNotAcquireSandbox(t *testing.T) {
	for _, tt := range []struct {
		name       string
		served     bool
		wantStatus int
	}{
		{name: "existing file", served: true, wantStatus: http.StatusOK},
		{name: "missing file", served: false, wantStatus: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := &fakeStaticFiles{served: tt.served}
			server := &Server{Log: testutils.TestLogger(t), staticFiles: files, aa: panicActivator{}}
			target := &resolvedIngressTarget{
				version: core_v1alpha.AppVersion{ID: entity.Id("version-1")},
				config:  &core_v1alpha.ConfigSpec{StaticDir: "/app/dist"},
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://example.com/asset.txt", nil)
			appName := "static-app"

			server.serveAuthenticatedRequest(recorder, request, entity.Id("app-1"), "web", "route", target, &appName, 0)

			assert.Equal(t, tt.wantStatus, recorder.Code)
			assert.Equal(t, 1, files.calls)
		})
	}
}

func TestStaticFileFailureReturnsServerError(t *testing.T) {
	files := &fakeStaticFiles{err: errors.New("snapshot unavailable")}
	server := &Server{Log: testutils.TestLogger(t), staticFiles: files, aa: panicActivator{}}
	target := &resolvedIngressTarget{
		version: core_v1alpha.AppVersion{ID: entity.Id("version-1")},
		config:  &core_v1alpha.ConfigSpec{StaticDir: "/app/dist"},
	}
	recorder := httptest.NewRecorder()
	appName := "static-app"

	server.serveAuthenticatedRequest(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/asset.txt", nil), entity.Id("app-1"), "web", "route", target, &appName, 0)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestMissingStaticFileFallsThroughToDeclaredService(t *testing.T) {
	files := &fakeStaticFiles{}
	appActivator := &recordingActivator{}
	server := &Server{Log: testutils.TestLogger(t), staticFiles: files, aa: appActivator}
	target := &resolvedIngressTarget{
		version: core_v1alpha.AppVersion{ID: entity.Id("version-1")},
		config: &core_v1alpha.ConfigSpec{
			StaticDir: "/app/dist",
			Services:  []core_v1alpha.ConfigSpecServices{{Name: "web"}},
		},
	}
	recorder := httptest.NewRecorder()
	appName := "mixed-app"

	server.serveAuthenticatedRequest(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/dynamic", nil), entity.Id("app-1"), "web", "route", target, &appName, 0)

	assert.True(t, appActivator.acquired)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestStaticFilesDoNotShadowNonWebService(t *testing.T) {
	files := &fakeStaticFiles{served: true}
	appActivator := &recordingActivator{}
	server := &Server{Log: testutils.TestLogger(t), staticFiles: files, aa: appActivator}
	target := &resolvedIngressTarget{
		version: core_v1alpha.AppVersion{ID: entity.Id("version-1")},
		config: &core_v1alpha.ConfigSpec{
			StaticDir: "/app/dist",
			Services:  []core_v1alpha.ConfigSpecServices{{Name: "api"}},
		},
	}
	recorder := httptest.NewRecorder()
	appName := "mixed-app"

	server.serveAuthenticatedRequest(recorder, httptest.NewRequest(http.MethodGet, "http://example.com/asset.txt", nil), entity.Id("app-1"), "api", "route", target, &appName, 0)

	assert.Zero(t, files.calls)
	assert.True(t, appActivator.acquired)
}
