package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"github.com/stretchr/testify/require"
	"github.com/tonistiigi/fsutil"
	"miren.dev/runtime/pkg/testutils"
)

func TestBuildKitLocal(t *testing.T) {
	t.Run("transforms a local directory into on oci tar", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(TestInject)

		defer cleanup()

		var bkl *Buildkit

		err := reg.Init(&bkl)
		r.NoError(err)

		dfr, err := MakeTar("testdata/df1")
		r.NoError(err)

		datafs, err := TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		files, err := TarToMap(o)
		r.NoError(err)

		var index struct {
			Manifests []struct {
				Digest string `json:"digest"`
			} `json:"manifests"`
		}

		r.NoError(json.Unmarshal(files["index.json"], &index))

		r.True(strings.HasPrefix(index.Manifests[0].Digest, "sha256:"))

		man := files["blobs/sha256/"+index.Manifests[0].Digest[7:]]

		// NOTE(emp): If SBOMS are added to the image in the future, this test will
		// break because sboms make the image appear to be multi platform. This code
		// path is hardcoded to expect a single platform image.

		var manIndex struct {
			Manifests []struct {
				Digest string `json:"digest"`
			} `json:"manifests"`

			Layers []struct {
				Digest string `json:"digest"`
			} `json:"layers"`
		}

		r.NoError(json.Unmarshal(man, &manIndex))

		layer := files["blobs/sha256/"+manIndex.Layers[0].Digest[7:]]

		spew.Dump(layer)

		gzr, err := gzip.NewReader(bytes.NewReader(layer))
		r.NoError(err)

		tr := tar.NewReader(gzr)

		th, err := tr.Next()
		r.NoError(err)

		r.Equal("note.txt", th.Name)

		x, err := io.ReadAll(tr)
		r.NoError(err)

		expected, err := os.ReadFile("testdata/df1/note.txt")
		r.NoError(err)

		r.Equal(string(expected), string(x))
	})

	t.Run("can handle large output tars", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(TestInject)
		defer cleanup()

		var bkl *Buildkit

		err := reg.Init(&bkl)
		r.NoError(err)

		datafs, err := fsutil.NewFS("testdata/df-large")
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		tr := tar.NewReader(o)

		var sz int64

		for {
			th, err := tr.Next()
			if err == io.EOF {
				break
			}

			sz += th.Size
		}

		r.Greater(int(sz), 1024*1024)

		r.NoError(o.Close())
	})
}
