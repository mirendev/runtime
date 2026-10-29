package build

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	buildkit "github.com/moby/buildkit/client"
	"github.com/stretchr/testify/require"
	"github.com/tonistiigi/fsutil"
	"miren.dev/runtime/components/ocireg"
)

func TestBuildImageRefreshesBaseImage(t *testing.T) {
	if _, err := os.Stat("/run/buildkit/buildkitd.sock"); err != nil {
		t.Skip("BuildKit daemon not available")
	}
	ctx := t.Context()
	bkc, err := buildkit.New(ctx, "unix:///run/buildkit/buildkitd.sock")
	require.NoError(t, err)
	defer bkc.Close()
	h := newDeploySagaHarness(t)
	registry := ocireg.NewRegistryHandler(t.TempDir(), h.builder.Log, h.builder.ec)
	var revision atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/manifests/current") {
			r.URL.Path = strings.TrimSuffix(r.URL.Path, "current") + fmt.Sprintf("base-%d", revision.Load())
		}
		registry.ServeHTTP(w, r)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	bk := &Buildkit{Client: bkc, Log: slog.Default()}
	build := func(dockerfile, tag string) *BuildResult {
		t.Helper()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile.miren"), []byte(dockerfile), 0644))
		dfs, err := fsutil.NewFS(dir)
		require.NoError(t, err)
		res, err := bk.BuildImage(ctx, dfs, BuildStack{Stack: "dockerfile", Input: "Dockerfile.miren", CodeDir: dir}, "demo", host+"/demo:"+tag)
		require.NoError(t, err)
		return res
	}
	old := build("FROM scratch\nWORKDIR /old-base\n", "base-0")
	build("FROM scratch\nWORKDIR /new-base\n", "base-1")
	first := build("FROM "+host+"/demo:current\n", "first")
	require.Equal(t, "/old-base", first.WorkingDir)
	revision.Store(1)
	second := build("FROM "+host+"/demo:current\n", "second")
	require.Equal(t, "/new-base", second.WorkingDir)
	require.NotEqual(t, first.ManifestDigest, second.ManifestDigest)
	pinned := build("FROM "+host+"/demo@"+old.ManifestDigest+"\n", "pinned")
	require.Equal(t, "/old-base", pinned.WorkingDir)
}
