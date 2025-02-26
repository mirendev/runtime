package lsvd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSegmentCreator(t *testing.T) {
	log := slog.Default()

	ctx := NewContext(context.Background())

	t.Run("logs writes to disk", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "oc")
		r.NoError(err)

		defer os.RemoveAll(tmpdir)

		path := filepath.Join(tmpdir, "log")

		oc, err := NewSegmentCreator(log, "", path)
		r.NoError(err)

		data := NewRangeData(ctx, Extent{47, 5})

		for i := range data.WriteData() {
			data.WriteData()[i] = byte(i)
		}

		err = oc.WriteExtent(data)
		r.NoError(err)

		f := oc.builder.logF

		_, err = f.Seek(0, io.SeekStart)
		r.NoError(err)

		oc2 := &SegmentCreator{
			log:     log,
			em:      NewExtentMap(),
			builder: NewSegmentBuilder(),
		}

		oc2.builder.em = oc2.em

		err = oc2.builder.readLog(f, log)
		r.NoError(err)
	})

	t.Run("can serve reads from the write cache", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "oc")
		r.NoError(err)

		defer os.RemoveAll(tmpdir)

		path := filepath.Join(tmpdir, "log")

		oc, err := NewSegmentCreator(log, "", path)
		r.NoError(err)

		data := NewRangeData(ctx, Extent{47, 5})

		d := data.WriteData()
		for i := range d {
			d[i] = byte(i)
		}

		err = oc.WriteExtent(data)
		r.NoError(err)

		readRequest := NewRangeData(ctx, Extent{48, 1})

		ret, err := oc.FillExtent(ctx, readRequest.View())
		r.NoError(err)

		r.Equal(data.ReadData()[BlockSize:BlockSize*2], readRequest.ReadData())

		r.Len(ret, 1)

		r.Equal(Extent{48, 1}, ret[0])
	})

	t.Run("serves read requests according to write order", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "oc")
		r.NoError(err)

		defer os.RemoveAll(tmpdir)

		path := filepath.Join(tmpdir, "log")

		oc, err := NewSegmentCreator(log, "", path)
		r.NoError(err)

		data := NewRangeData(ctx, Extent{47, 5})

		d := data.WriteData()
		for i := range d {
			d[i] = byte(i)
		}

		err = oc.WriteExtent(data)
		r.NoError(err)

		d2 := NewRangeData(ctx, Extent{48, 1})

		d = d2.WriteData()
		for i := range d {
			d[i] = byte(i + 1)
		}

		err = oc.WriteExtent(d2)
		r.NoError(err)

		req := NewRangeData(ctx, Extent{48, 2})

		ret, err := oc.FillExtent(ctx, req.View())
		r.NoError(err)

		r.Equal(d2.ReadData()[:BlockSize], req.ReadData()[:BlockSize])
		r.Equal(data.ReadData()[BlockSize*3:BlockSize*4], req.ReadData()[BlockSize:])

		r.Len(ret, 2)

		r.Equal(Extent{48, 1}, ret[0])
		r.Equal(Extent{49, 1}, ret[1])
	})
}
