package lsvd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRangeData(t *testing.T) {
	ctx := NewContext(context.Background())

	t.Run("slice for subrange", func(t *testing.T) {
		r := require.New(t)

		rd := NewRangeData(ctx, Extent{0, 20})

		sub, ok := rd.SubRange(Extent{10, 10})
		r.True(ok)

		data := sub.WriteData()

		r.Len(data, 10*BlockSize)

		data[0] = 8
		data[len(data)-1] = 9

		r.Equal(byte(8), rd.data[10*BlockSize])
		r.Equal(byte(9), rd.data[(20*BlockSize)-1])
	})

	t.Run("clamps the requested range", func(t *testing.T) {
		r := require.New(t)

		rd := NewRangeData(ctx, Extent{5, 200})

		sub, ok := rd.SubRange(Extent{0, 10})
		r.True(ok)

		r.Equal(Extent{5, 5}, sub.Extent)

		data := sub.WriteData()

		r.Len(data, 5*BlockSize)

		data[0] = 8
		data[len(data)-1] = 9

		r.Equal(byte(8), rd.data[0])
		r.Equal(byte(9), rd.data[(5*BlockSize)-1])
	})

	t.Run("can copy from an empty range", func(t *testing.T) {
		r := require.New(t)

		x := NewRangeData(ctx, Extent{0, 10})

		out := make([]byte, BlockSize*10)

		for i := range out {
			out[i] = 8
		}

		err := x.CopyTo(out)
		r.NoError(err)

		r.True(isEmpty(out))
	})
}

func BenchmarkCopyEmpty(b *testing.B) {
	ctx := NewContext(context.Background())

	x := NewRangeData(ctx, Extent{0, 10})

	out := make([]byte, BlockSize*10)
	for i := range out {
		out[i] = 8
	}

	for i := 0; i < b.N; i++ {
		x.CopyTo(out)
	}
}
