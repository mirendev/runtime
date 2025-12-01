package lsvd

import (
	"context"
	"sync"
)

const BufferSliceSize = 1024 * 1024

// MaxPoolBufferSize is the maximum size of a buffer that will be returned to the pool.
// Buffers larger than this are discarded to prevent memory bloat.
const MaxPoolBufferSize = 8 * 1024 * 1024

type Buffers struct {
	slice []byte

	next int
}

var buffersPool = sync.Pool{
	New: func() any {
		return &Buffers{
			slice: make([]byte, BufferSliceSize),
		}
	},
}

func NewBuffers() *Buffers {
	return buffersPool.Get().(*Buffers)
}

func ReturnBuffers(buf *Buffers) {
	buf.next = 0
	// Don't return oversized buffers to the pool - let them be GC'd
	if len(buf.slice) <= MaxPoolBufferSize {
		buffersPool.Put(buf)
	}
}

type buffersKey struct{}

func B(ctx context.Context) *Buffers {
	val := ctx.Value(buffersKey{})
	if val == nil {
		return &Buffers{}
	}

	return val.(*Buffers)
}

func (b *Buffers) Inject(ctx context.Context) context.Context {
	return context.WithValue(ctx, buffersKey{}, b)
}

func (b *Buffers) Reset() {
	clear(b.slice[:b.next])
	b.next = 0
}

func (b *Buffers) Marker() int {
	return b.next
}

func (b *Buffers) ResetTo(marker int) {
	b.next = marker
}

func (b *Buffers) alloc(sz int) []byte {
	if len(b.slice)-b.next < sz {
		if sz > BufferSliceSize {
			return make([]byte, sz)
		}

		dup := make([]byte, len(b.slice)+BufferSliceSize)
		copy(dup, b.slice)
		b.slice = dup
	}

	data := b.slice[b.next : b.next+sz]
	b.next += sz

	return data
}
