package build

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/moby/buildkit/util/contentutil"
	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDirectImageDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type staticImageMetadataResolver struct {
	digest digest.Digest
	config []byte
	err    error
	ref    string
}

func (r *staticImageMetadataResolver) Resolve(_ context.Context, ref string, _ ocispecs.Platform) (digest.Digest, []byte, error) {
	r.ref = ref
	return r.digest, r.config, r.err
}

func TestResolveDirectImage(t *testing.T) {
	resolver := &staticImageMetadataResolver{
		digest: digest.Digest(testDirectImageDigest),
		config: []byte(`{
			"config": {
				"WorkingDir": "/srv/app",
				"ExposedPorts": {"4321/tcp": {}},
				"Entrypoint": ["/entrypoint"],
				"Cmd": ["serve"]
			}
		}`),
	}
	b := &Builder{imageMetadataResolver: resolver}

	image, result, err := b.resolveDirectImage(t.Context(), "example/app:latest")
	require.NoError(t, err)

	assert.Equal(t, "docker.io/example/app:latest", resolver.ref)
	assert.Equal(t, "docker.io/example/app@"+testDirectImageDigest, image)
	assert.Equal(t, testDirectImageDigest, result.ManifestDigest)
	assert.Equal(t, "/srv/app", result.WorkingDir)
	assert.Equal(t, []string{"4321/tcp"}, result.ExposedPorts)
	assert.Empty(t, result.Entrypoint, "the image entrypoint remains in OCI config for exec-form launch")
	assert.Empty(t, result.Command, "the image command remains in OCI config for exec-form launch")
}

func TestResolveDirectImageDefaultsWorkingDirectoryToRoot(t *testing.T) {
	b := &Builder{imageMetadataResolver: &staticImageMetadataResolver{
		digest: digest.Digest(testDirectImageDigest),
		config: []byte(`{"config": {}}`),
	}}

	_, result, err := b.resolveDirectImage(t.Context(), "busybox:latest")
	require.NoError(t, err)
	assert.Equal(t, "/", result.WorkingDir)
}

func TestResolveDirectImageFailsClosed(t *testing.T) {
	t.Run("resolver error", func(t *testing.T) {
		b := &Builder{imageMetadataResolver: &staticImageMetadataResolver{err: errors.New("registry unavailable")}}
		_, _, err := b.resolveDirectImage(t.Context(), "busybox:latest")
		require.ErrorContains(t, err, "registry unavailable")
	})

	t.Run("invalid config", func(t *testing.T) {
		b := &Builder{imageMetadataResolver: &staticImageMetadataResolver{
			digest: digest.Digest(testDirectImageDigest),
			config: []byte(`not json`),
		}}
		_, _, err := b.resolveDirectImage(t.Context(), "busybox:latest")
		require.ErrorContains(t, err, "reading config")
	})
}

func TestSelectedManifestDigest(t *testing.T) {
	t.Run("follows the one fetched platform branch", func(t *testing.T) {
		buffer := contentutil.NewBuffer()
		manifest := writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageManifest, ocispecs.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispecs.MediaTypeImageManifest,
		})
		other := ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageManifest,
			Digest:    digest.FromString("not fetched"),
			Size:      1,
		}
		index := writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageIndex, ocispecs.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispecs.MediaTypeImageIndex,
			Manifests: []ocispecs.Descriptor{other, manifest},
		})

		got, err := selectedManifestDigest(t.Context(), buffer, index.Digest)
		require.NoError(t, err)
		assert.Equal(t, manifest.Digest, got)
	})

	t.Run("rejects multiple fetched branches", func(t *testing.T) {
		buffer := contentutil.NewBuffer()
		first := writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageManifest, ocispecs.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispecs.MediaTypeImageManifest,
		})
		second := writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageManifest, map[string]any{
			"schemaVersion": 2,
			"mediaType":     ocispecs.MediaTypeImageManifest,
			"annotations":   map[string]string{"distinct": "manifest"},
		})
		index := writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageIndex, ocispecs.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispecs.MediaTypeImageIndex,
			Manifests: []ocispecs.Descriptor{first, second},
		})

		_, err := selectedManifestDigest(t.Context(), buffer, index.Digest)
		require.ErrorContains(t, err, "expected one selected child")
	})

	t.Run("bounds nested indexes", func(t *testing.T) {
		buffer := contentutil.NewBuffer()
		child := writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageManifest, ocispecs.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispecs.MediaTypeImageManifest,
		})
		for range maxManifestIndexNesting + 1 {
			child = writeImageJSONBlob(t, buffer, ocispecs.MediaTypeImageIndex, ocispecs.Index{
				Versioned: specs.Versioned{SchemaVersion: 2},
				MediaType: ocispecs.MediaTypeImageIndex,
				Manifests: []ocispecs.Descriptor{child},
			})
		}

		_, err := selectedManifestDigest(t.Context(), buffer, child.Digest)
		require.ErrorContains(t, err, "manifest index nesting exceeds maximum depth")
	})
}

func writeImageJSONBlob(t *testing.T, buffer contentutil.Buffer, mediaType string, value any) ocispecs.Descriptor {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	desc := ocispecs.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	require.NoError(t, content.WriteBlob(t.Context(), buffer, desc.Digest.Encoded(), bytes.NewReader(data), desc))
	return desc
}
