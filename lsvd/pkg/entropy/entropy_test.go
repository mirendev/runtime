package entropy

import (
	"crypto/rand"
	"io"
	"testing"

	"github.com/pierrec/lz4/v4"
	"github.com/stretchr/testify/require"
)

func TestEntropy(t *testing.T) {
	t.Run("empty blocks are low", func(t *testing.T) {
		r := require.New(t)

		e := NewEstimator()

		e.Write(make([]byte, 4096))

		r.Equal(0.0, e.Value())
	})

	t.Run("random blocks are high", func(t *testing.T) {
		r := require.New(t)

		e := NewEstimator()

		data := make([]byte, 4096)

		io.ReadFull(rand.Reader, data)

		e.Write(data)

		r.Greater(e.Value(), 5.0)
	})

	t.Run("sparse blocks are low", func(t *testing.T) {
		r := require.New(t)

		e := NewEstimator()

		data := make([]byte, 4096)

		copy(data, []byte("hello"))

		e.Write(data)

		r.Less(e.Value(), 1.0)
	})

	t.Run("uniform blocks are low", func(t *testing.T) {
		r := require.New(t)

		e := NewEstimator()

		data := make([]byte, 4096)

		for i := range data {
			data[i] = byte(i) % 2
		}

		e.Write(data)

		r.Less(e.Value(), 2.0)
	})
}

func BenchmarkEntropy(b *testing.B) {
	e := NewEstimator()

	data := make([]byte, 4096)

	for i := range data {
		data[i] = byte(i)
	}

	for i := 0; i < b.N; i++ {
		e.Write(data)
	}
}

func BenchmarkEntropyToLZ4(b *testing.B) {
	data := make([]byte, 4096)

	for i := range data {
		data[i] = byte(i)
	}

	dest := make([]byte, 4096)
	for i := 0; i < b.N; i++ {
		lz4.CompressBlock(data, dest, nil)
	}
}
