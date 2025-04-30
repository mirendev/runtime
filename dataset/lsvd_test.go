package dataset_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/dataset"
	"miren.dev/runtime/lsvd"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
)

var (
	testRand  = make([]byte, 4*1024)
	testRandX lsvd.RawBlocks
)

func init() {
	io.ReadFull(rand.Reader, testRand)
	testRandX = lsvd.BlockDataView(testRand)
}

func extentEqual(t *testing.T, actual lsvd.RawBlocks, expected lsvd.RangeData) {
	t.Helper()

	require.Equal(t, actual.Blocks(), expected.Blocks)

	if !bytes.Equal(actual, expected.ReadData()) {
		t.Error("blocks are not the same")
	}
}

func TestLSVD(t *testing.T) {
	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	gctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx := lsvd.NewContext(gctx)

	t.Run("can be used by lsvd", func(t *testing.T) {
		r := require.New(t)

		tmpDir := t.TempDir()
		manager, err := dataset.NewManager(log, tmpDir, "")
		r.NoError(err)

		// Create dataset
		mc := &dataset.DataSetsClient{
			NetworkClient: rpc.LocalClient(dataset.AdaptDataSets(manager)),
		}

		sa := dataset.NewSegmentAccess(log, mc, []string{"application/test"})

		d, err := lsvd.NewDisk(ctx, log, t.TempDir(),
			lsvd.WithSegmentAccess(sa),
		)

		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		t.Log("closing")
		r.NoError(d.Close(ctx))

		res, err := mc.Get(ctx, "default")
		r.NoError(err)

		ds := res.Dataset()

		sres, err := ds.ListSegments(ctx)
		r.NoError(err)

		r.Equal(1, len(sres.Segments()))

		t.Logf("segment: %s", sres.Segments()[0])

		t.Log("reopening disk")
		d, err = lsvd.NewDisk(ctx, log, t.TempDir(),
			lsvd.WithSegmentAccess(sa),
		)
		r.NoError(err)
		defer d.Close(ctx)

		d2, err := d.ReadExtent(ctx, lsvd.Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d2)

		ires, err := ds.GetInfo(ctx)
		r.NoError(err)

		info := ires.Info()

		r.Equal([]string{
			"block/raw",
			"fs/ext4",
			"application/test",
		}, info.Formats())
	})
}
