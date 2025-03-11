package lsvd

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestNBD(t *testing.T) {
	log := slog.Default()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var testRand RawBlocks = make([]byte, 4*1024)

	_, err := io.ReadFull(rand.Reader, testRand)
	if err != nil {
		panic(err)
	}

	t.Run("returns data from the cache", func(t *testing.T) {
		r := require.New(t)

		dir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(dir)

		d, err := NewDisk(ctx, log, dir)
		r.NoError(err)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		d, err = NewDisk(ctx, log, dir)
		r.NoError(err)

		b := NBDWrapper(ctx, log, d)

		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		r.NoError(err)

		rp := os.NewFile(uintptr(fds[0]), "sp1")
		wp := os.NewFile(uintptr(fds[1]), "sp2")

		defer rp.Close()
		defer wp.Close()

		buf := make([]byte, BlockSize)
		ok, err := b.ReadIntoConn(buf, 0, wp)
		r.NoError(err)

		wp.Close()

		r.True(ok)

		data, err := io.ReadAll(rp)
		r.NoError(err)

		blockEqual(t, testRandX, data)
	})

	t.Run("attempts to build larger write extents", func(t *testing.T) {
		r := require.New(t)

		dir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(dir)

		d, err := NewDisk(ctx, log, dir)
		r.NoError(err)

		b := NBDWrapper(ctx, log, d)

		n, err := b.WriteAt(testRand, 0)
		r.NoError(err)
		r.Equal(len(testRand), n)

		r.Equal(Extent{0, 1}, b.pendingWrite)
		r.Equal([]byte(testRand), b.pendingWriteData.Bytes())

		n, err = b.WriteAt(testRand, BlockSize)
		r.NoError(err)
		r.Equal(len(testRand), n)

		r.Equal(Extent{0, 2}, b.pendingWrite)
		r.Equal(BlockSize*2, b.pendingWriteData.Len())
		r.Equal([]byte(testRand), b.pendingWriteData.Bytes()[:BlockSize])
		r.Equal([]byte(testRand), b.pendingWriteData.Bytes()[BlockSize:])

		n, err = b.WriteAt(testRand, BlockSize*10)
		r.NoError(err)
		r.Equal(len(testRand), n)

		r.Equal(Extent{10, 1}, b.pendingWrite)
		r.Equal(BlockSize, b.pendingWriteData.Len())
		r.Equal([]byte(testRand), b.pendingWriteData.Bytes())

		err = b.Trim(0, BlockSize)
		r.NoError(err)

		r.Equal(Extent{0, 0}, b.pendingWrite)
		r.Equal(0, b.pendingWriteData.Len())
		r.Equal(Extent{0, 1}, b.pendingTrim)

		n, err = b.WriteAt(testRand, 0)
		r.NoError(err)
		r.Equal(len(testRand), n)

		r.Equal(Extent{0, 0}, b.pendingTrim)
		r.Equal(Extent{0, 1}, b.pendingWrite)
		r.Equal(BlockSize, b.pendingWriteData.Len())
		r.Equal([]byte(testRand), b.pendingWriteData.Bytes())

		err = b.Trim(0, BlockSize)
		r.NoError(err)

		r.Equal(Extent{0, 1}, b.pendingTrim)

		err = b.Trim(0, 2*BlockSize)
		r.NoError(err)

		r.Equal(Extent{0, 2}, b.pendingTrim)
	})

	t.Run("properly handles back to back large trims", func(t *testing.T) {
		r := require.New(t)

		dir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(dir)

		d, err := NewDisk(ctx, log, dir)
		r.NoError(err)

		b := NBDWrapper(ctx, log, d)

		err = b.Trim(0, (math.MaxUint16-5)*BlockSize)
		r.NoError(err)

		err = b.Trim((math.MaxUint16-5)*BlockSize, 100*BlockSize)
		r.NoError(err)

		r.Less(b.pendingTrim.Blocks, uint32(math.MaxUint16))

		err = b.flushPendingWrite()
		r.NoError(err)
	})
}
