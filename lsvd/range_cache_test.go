package lsvd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var nullSeg SegmentId

func TestRangeCache(t *testing.T) {
	t.Run("requests large ranges to serve small ones", func(t *testing.T) {
		r := require.New(t)
		path := filepath.Join(t.TempDir(), "blah")

		var fetchCalls int

		ctx := context.TODO()

		rc, err := NewRangeCache(
			RangeCacheOptions{
				Path:      path,
				MaxSize:   1024 * 1024,
				ChunkSize: 1024,
				Fetch: func(ctx context.Context, _ SegmentId, data []byte, off int64) error {
					fetchCalls++
					r.Len(data, 1024)
					r.Equal(int64(0), off)

					for i := range data {
						data[i] = byte(i)
					}

					return nil
				},
			},
		)
		r.NoError(err)

		defer rc.Close()

		buf := make([]byte, 3)
		n, err := rc.ReadAt(ctx, nullSeg, buf, 2)
		r.NoError(err)

		r.Equal(1, fetchCalls)

		r.Equal(3, n)

		r.Equal(byte(2), buf[0])
		r.Equal(byte(3), buf[1])
		r.Equal(byte(4), buf[2])

		buf2 := make([]byte, 3)
		n2, err := rc.ReadAt(ctx, nullSeg, buf2, 2)
		r.NoError(err)

		r.Equal(1, fetchCalls)

		r.Equal(3, n2)

		r.Equal(buf, buf2)
	})

	t.Run("can request 2 chunks if needed", func(t *testing.T) {
		r := require.New(t)
		path := filepath.Join(t.TempDir(), "blah")

		var fetchCalls int

		ctx := context.TODO()

		rc, err := NewRangeCache(
			RangeCacheOptions{
				Path:      path,
				MaxSize:   100,
				ChunkSize: 10,
				Fetch: func(ctx context.Context, _ SegmentId, data []byte, off int64) error {
					fetchCalls++
					r.Len(data, 10)

					switch fetchCalls {
					case 1:
						r.Equal(int64(0), off)
					case 2:
						r.Equal(int64(10), off)
					case 3:
						r.Fail("too many fetch calls")
					}

					for i := range data {
						data[i] = byte(i)
					}

					return nil
				},
			},
		)
		r.NoError(err)

		defer rc.Close()

		buf := make([]byte, 4)
		n, err := rc.ReadAt(ctx, nullSeg, buf, 9)
		r.NoError(err)

		r.Equal(2, fetchCalls)

		r.Equal(4, n)

		r.Equal(byte(9), buf[0])
		r.Equal(byte(0), buf[1])
		r.Equal(byte(1), buf[2])
		r.Equal(byte(2), buf[3])
	})

	t.Run("can request positions in the cache for an extent", func(t *testing.T) {
		r := require.New(t)
		path := filepath.Join(t.TempDir(), "blah")

		var fetchCalls int

		ctx := context.TODO()

		rc, err := NewRangeCache(
			RangeCacheOptions{
				Path:      path,
				MaxSize:   100,
				ChunkSize: 10,
				Fetch: func(ctx context.Context, _ SegmentId, data []byte, off int64) error {
					fetchCalls++
					r.Len(data, 10)

					switch fetchCalls {
					case 1:
						r.Equal(int64(0), off)
					case 2:
						r.Equal(int64(10), off)
					case 3:
						r.Fail("too many fetch calls")
					}

					for i := range data {
						data[i] = byte(i)
					}

					return nil
				},
			},
		)
		r.NoError(err)

		defer rc.Close()

		p, err := rc.CachePositions(ctx, nullSeg, 4, 9, nil)
		r.NoError(err)

		r.Equal(2, fetchCalls)

		r.Len(p, 2)

		r.Equal(int64(9), p[0].off)
		r.Equal(int64(1), p[0].size)

		r.Equal(int64(10), p[1].off)
		r.Equal(int64(3), p[1].size)
	})

	t.Run("discards old chunks when needed", func(t *testing.T) {
		r := require.New(t)
		path := filepath.Join(t.TempDir(), "blah")

		var fetchCalls int

		ctx := context.TODO()

		rc, err := NewRangeCache(
			RangeCacheOptions{
				Path:      path,
				MaxSize:   10,
				ChunkSize: 1,
				Fetch: func(ctx context.Context, seg SegmentId, data []byte, off int64) error {
					fetchCalls++

					data[0] = byte(off)
					return nil
				},
			},
		)
		r.NoError(err)

		defer rc.Close()

		for i := 0; i < 15; i++ {
			buf := make([]byte, 1)
			_, err := rc.ReadAt(ctx, nullSeg, buf, int64(i))
			r.NoError(err)
		}

		r.Equal(15, fetchCalls)

		r.Equal(10, rc.lru.Len())

		sz, err := rc.f.Stat()
		r.NoError(err)

		r.Equal(int64(10), sz.Size())
	})
}
