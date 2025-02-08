package slogfmt

import (
	"sync"
	"unicode/utf8"
)

// buffer adapted from go/src/fmt/print.go
type Buffer []byte

// Having an initial size gives a dramatic speedup.
var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return (*Buffer)(&b)
	},
}

func NewBuffer() *Buffer {
	buf := bufPool.Get().(*Buffer)
	buf.Reset()
	return buf
}

func (b *Buffer) Free() {
	// To reduce peak allocation, return only smaller buffers to the pool.
	const maxBufferSize = 16 << 10
	if cap(*b) <= maxBufferSize {
		*b = (*b)[:0]
		bufPool.Put(b)
	}
}

func (b *Buffer) Reset() {
	b.SetLen(0)
}

func (b *Buffer) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}

func (b *Buffer) WriteString(s string) (int, error) {
	*b = append(*b, s...)
	return len(s), nil
}

func (b *Buffer) WriteByte(c byte) error {
	*b = append(*b, c)
	return nil
}

func (b *Buffer) WriteNewLine() {
	if len(*b) > 0 && (*b)[0] == '\n' {
		return
	}

	*b = append(*b, '\n')
}

func (b *Buffer) WriteRune(r rune) error {
	// Compare as uint32 to correctly handle negative runes.
	if uint32(r) < utf8.RuneSelf {
		b.WriteByte(byte(r))
		return nil
	}
	*b = utf8.AppendRune(*b, r)
	return nil
}

func (b *Buffer) String() string {
	return string(*b)
}

func (b *Buffer) Len() int {
	return len(*b)
}

func (b *Buffer) SetLen(n int) {
	*b = (*b)[:n]
}
