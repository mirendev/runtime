package stackbuild

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/buildkit/solver/pb"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// A real metadata resolver talking to a mutable registry tag guards against
// accidentally replacing the per-build resolver with its process-wide Default.
func TestBaseImageRefreshesMutableTag(t *testing.T) {
	type blob struct {
		data []byte
		kind string
	}
	blobs := map[string]blob{}
	var manifests []digest.Digest
	for _, path := range []string{"/old/bin", "/new/bin"} {
		cfg := imageConfigJSON(t, []string{"PATH=" + path})
		cfgDigest := digest.FromBytes(cfg)
		manifest, err := json.Marshal(ocispecs.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispecs.MediaTypeImageManifest,
			Config: ocispecs.Descriptor{
				MediaType: ocispecs.MediaTypeImageConfig,
				Digest:    cfgDigest,
				Size:      int64(len(cfg)),
			},
			Layers: []ocispecs.Descriptor{},
		})
		require.NoError(t, err)
		manifestDigest := digest.FromBytes(manifest)
		blobs[cfgDigest.String()] = blob{cfg, ocispecs.MediaTypeImageConfig}
		blobs[manifestDigest.String()] = blob{manifest, ocispecs.MediaTypeImageManifest}
		manifests = append(manifests, manifestDigest)
	}
	var revision atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if key == "latest" {
			key = manifests[revision.Load()].String()
		}
		b, ok := blobs[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", b.kind)
		w.Header().Set("Content-Length", strconv.Itoa(len(b.data)))
		w.Header().Set("Docker-Content-Digest", key)
		if r.Method != http.MethodHead {
			_, _ = w.Write(b.data)
		}
	}))
	defer server.Close()
	ref := strings.TrimPrefix(server.URL, "http://") + "/base:latest"

	check := func(ref string, want digest.Digest, path string) {
		t.Helper()
		var stack MetaStack
		stack.setupResult()
		state := stack.baseImage(t.Context(), ref, BuildOptions{})
		def, err := state.Marshal(t.Context())
		require.NoError(t, err)
		require.Equal(t, path, pathFromEnv(stack.Image().Config.Env))
		var source string
		for _, dt := range def.Def {
			var op pb.Op
			require.NoError(t, op.Unmarshal(dt))
			if s := op.GetSource(); s != nil {
				source = s.Identifier
			}
		}
		require.Contains(t, source, "@"+want.String())
	}
	check(ref, manifests[0], "/old/bin")
	revision.Store(1)
	check(ref, manifests[1], "/new/bin")
	// A digest supplied by the caller stays pinned even after the tag moves.
	check(ref+"@"+manifests[0].String(), manifests[0], "/old/bin")
}
