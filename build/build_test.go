package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/mapx"
)

func TestBuildKitLocal(t *testing.T) {
	t.Run("transforms a local directory into on oci tar", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		cl, err := client.New(ctx, "docker-container://test-buildkit")
		r.NoError(err)

		bkl, err := NewBuildkit(ctx, cl, t.TempDir())
		r.NoError(err)

		dfr, err := MakeTar("testdata/df1")
		r.NoError(err)

		o, err := bkl.Transform(ctx, dfr)
		r.NoError(err)

		files, err := TarToMap(o)
		r.NoError(err)

		r.Equal(
			[]string{
				"blobs/sha256/1525ad1245605a35496e04e83d6e6324fe5cf9684b2ffe5d25ed2fd45f01b01d",
				"blobs/sha256/1c02178e06486d511a23db8de02c345cdaab3feaabc8bd74bed5b292b80d5748",
				"blobs/sha256/94ec9ce2a0ea8220b80a1aceed29a48fa90ad9a805cc7a82bc0ba3a360c388ab",
				"index.json", "oci-layout"},
			mapx.Keys(files),
		)

		gzr, err := gzip.NewReader(bytes.NewReader(files["blobs/sha256/1c02178e06486d511a23db8de02c345cdaab3feaabc8bd74bed5b292b80d5748"]))
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
}
