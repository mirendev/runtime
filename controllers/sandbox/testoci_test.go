package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

type testImageFile struct {
	Path    string // path in image layer (no leading slash)
	Content []byte
	IsDir   bool
}

// buildGoOCIImage compiles the Go package at sourceDir into a static binary,
// then produces an OCI image layout tar containing that binary at binaryPath.
// extraFiles are added to the layer alongside the binary.
func buildGoOCIImage(t *testing.T, sourceDir, binaryPath string, extraFiles []testImageFile, configOpts ...func(*ocispecs.Image)) io.ReadCloser {
	t.Helper()
	r := require.New(t)

	tmpDir := t.TempDir()
	binOut := filepath.Join(tmpDir, "binary")

	// Resolve sourceDir to absolute so go build runs in the right module.
	absDir, err := filepath.Abs(sourceDir)
	r.NoError(err)

	// Compile the Go package as a static binary.
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-trimpath", "-o", binOut, ".")
	cmd.Dir = absDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	r.NoError(cmd.Run(), "go build failed")

	binData, err := os.ReadFile(binOut)
	r.NoError(err)

	// Build the gzipped layer tar.
	layerBuf := buildLayer(t, binaryPath, binData, extraFiles)

	layerDigest := digest.FromBytes(layerBuf.Bytes())

	// diffID is the digest of the uncompressed layer tar.
	diffID := uncompressedDigest(t, layerBuf.Bytes())

	// Build OCI image config.
	config := ocispecs.Image{
		Platform: ocispecs.Platform{
			OS:           "linux",
			Architecture: runtime.GOARCH,
		},
		Config: ocispecs.ImageConfig{
			Entrypoint: []string{binaryPath},
		},
		RootFS: ocispecs.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{diffID},
		},
	}
	for _, opt := range configOpts {
		opt(&config)
	}
	configJSON, err := json.Marshal(config)
	r.NoError(err)
	configDigest := digest.FromBytes(configJSON)

	// Build OCI manifest.
	manifest := ocispecs.Manifest{
		MediaType: ocispecs.MediaTypeImageManifest,
		Config: ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageConfig,
			Digest:    configDigest,
			Size:      int64(len(configJSON)),
		},
		Layers: []ocispecs.Descriptor{
			{
				MediaType: ocispecs.MediaTypeImageLayerGzip,
				Digest:    layerDigest,
				Size:      int64(layerBuf.Len()),
			},
		},
	}
	manifest.SchemaVersion = 2
	manifestJSON, err := json.Marshal(manifest)
	r.NoError(err)
	manifestDigest := digest.FromBytes(manifestJSON)

	// Build OCI index.
	index := ocispecs.Index{
		MediaType: ocispecs.MediaTypeImageIndex,
		Manifests: []ocispecs.Descriptor{
			{
				MediaType: ocispecs.MediaTypeImageManifest,
				Digest:    manifestDigest,
				Size:      int64(len(manifestJSON)),
				Platform: &ocispecs.Platform{
					OS:           "linux",
					Architecture: runtime.GOARCH,
				},
			},
		},
	}
	index.SchemaVersion = 2
	indexJSON, err := json.Marshal(index)
	r.NoError(err)

	// Write OCI image layout tar.
	var out bytes.Buffer
	tw := tar.NewWriter(&out)

	writeEntry := func(name string, data []byte) {
		r.NoError(tw.WriteHeader(&tar.Header{
			Name: name,
			Size: int64(len(data)),
			Mode: 0644,
		}))
		_, err := tw.Write(data)
		r.NoError(err)
	}

	ociLayout, _ := json.Marshal(ocispecs.ImageLayout{Version: ocispecs.ImageLayoutVersion})
	writeEntry("oci-layout", ociLayout)

	// Create directory entries for blobs/sha256/
	for _, dir := range []string{"blobs/", "blobs/sha256/"} {
		r.NoError(tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     dir,
			Mode:     0755,
		}))
	}

	writeEntry("blobs/sha256/"+layerDigest.Encoded(), layerBuf.Bytes())
	writeEntry("blobs/sha256/"+configDigest.Encoded(), configJSON)
	writeEntry("blobs/sha256/"+manifestDigest.Encoded(), manifestJSON)
	writeEntry("index.json", indexJSON)

	r.NoError(tw.Close())

	return io.NopCloser(&out)
}

// buildLayer creates a gzipped tar containing the binary and extra files.
func buildLayer(t *testing.T, binaryPath string, binData []byte, extraFiles []testImageFile) *bytes.Buffer {
	t.Helper()
	r := require.New(t)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Strip leading slash for tar path.
	tarPath := binaryPath
	if len(tarPath) > 0 && tarPath[0] == '/' {
		tarPath = tarPath[1:]
	}

	// Ensure parent directories exist in the tar.
	dir := filepath.Dir(tarPath)
	if dir != "." {
		r.NoError(tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     dir + "/",
			Mode:     0755,
		}))
	}

	r.NoError(tw.WriteHeader(&tar.Header{
		Name: tarPath,
		Size: int64(len(binData)),
		Mode: 0755,
	}))
	_, err := tw.Write(binData)
	r.NoError(err)

	createdDirs := map[string]bool{}
	if dir != "." {
		createdDirs[dir+"/"] = true
	}

	for _, f := range extraFiles {
		// Ensure parent directories exist.
		if f.IsDir {
			parts := strings.Split(f.Path, "/")
			for i := 1; i <= len(parts); i++ {
				d := strings.Join(parts[:i], "/") + "/"
				if !createdDirs[d] {
					r.NoError(tw.WriteHeader(&tar.Header{
						Typeflag: tar.TypeDir,
						Name:     d,
						Mode:     0755,
					}))
					createdDirs[d] = true
				}
			}
		} else {
			r.NoError(tw.WriteHeader(&tar.Header{
				Name: f.Path,
				Size: int64(len(f.Content)),
				Mode: 0644,
			}))
			_, err := tw.Write(f.Content)
			r.NoError(err)
		}
	}

	r.NoError(tw.Close())
	r.NoError(gw.Close())
	return &buf
}

// uncompressedDigest computes the digest of the uncompressed content of gzipped data.
func uncompressedDigest(t *testing.T, gzipped []byte) digest.Digest {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(gzipped))
	require.NoError(t, err)
	defer gr.Close()
	data, err := io.ReadAll(gr)
	require.NoError(t, err)
	return digest.FromBytes(data)
}
