package lsvd

import (
	"bytes"
	"fmt"
	"io"
	"slices"
)

type (
	RawBlocks []byte

	RangeData struct {
		Extent
		data  RawBlocks
		dirty bool
	}
)

const (
	smallRangeBlocks = 20
	smallRange       = BlockSize * smallRangeBlocks
)

func (e RawBlocks) Blocks() uint32 {
	return uint32(len(e) / BlockSize)
}

func (e RawBlocks) MapTo(lba LBA) RangeData {
	return RangeData{
		Extent: Extent{lba, uint32(e.Blocks())},
		data:   e,
	}
}

func BlockDataView(blk []byte) RawBlocks {
	cnt := len(blk) / BlockSize
	if cnt < 0 || len(blk)%BlockSize != 0 {
		panic("invalid block data size for extent")
	}

	return RawBlocks(slices.Clone(blk))
}

func (e *RangeData) CopyTo(data []byte) error {
	if e.data == nil {
		clear(data[:e.ByteSize()])
	} else {
		copy(data, e.data)
	}

	e.dirty = true

	return nil
}

func (e *RangeData) RawBlocks() RawBlocks {
	if e.data == nil {
		panic("accessing unallocated range data")
	}

	e.dirty = true

	return e.data
}

func (e RawBlocks) BlockView(cnt int) []byte {
	return e[BlockSize*cnt : (BlockSize*cnt)+BlockSize]
}

func NewRangeData(ctx *Context, ext Extent) RangeData {
	return RangeData{
		Extent: ext,
		data:   ctx.Allocate(ext.ByteSize()),
	}
}

func AlignToBlock(b []byte) []byte {
	if len(b)%BlockSize == 0 {
		return b
	}

	sized := len(b) + (BlockSize - (len(b) % BlockSize))

	rest := make([]byte, sized)
	copy(rest, b)

	return rest
}

func MapRangeData(ext Extent, srcData []byte) RangeData {
	if len(srcData)%BlockSize != 0 {
		panic(fmt.Sprintf("invalid input byte array, not block sized, %d", len(srcData)))
	}

	if ext.LBA > MaxLBA {
		panic("extent LBA larger than max")
	}

	if ext.Blocks > MaxBlocks {
		panic("extent blocks larger than max")
	}

	return RangeData{
		Extent: ext,
		data:   srcData,
		dirty:  true,
	}
}

func (r *RangeData) WriteData() []byte {
	if r.data == nil {
		panic("writing to unallocated range data")
	}

	r.dirty = true

	return r.data
}

func (r *RangeData) ReadData() []byte {
	if r.data == nil {
		panic("attempting to inflate empty range data")
	}

	return r.data
}

func (r *RangeData) EmptyP() bool {
	if r.dirty {
		return false
	}

	return emptyBytes(r.data)
}

type RangeDataView struct {
	Extent
	r *RangeData

	start, end int
}

func (r *RangeData) SubRange(ext Extent) (RangeDataView, bool) {
	ext, ok := r.Clamp(ext)
	if !ok {
		return RangeDataView{}, false
	}

	byteOffset := (ext.LBA - r.LBA) * BlockSize
	byteEnd := byteOffset + LBA(ext.Blocks*BlockSize)

	return RangeDataView{
		Extent: ext,
		r:      r,
		start:  int(byteOffset),
		end:    int(byteEnd),
	}, true
}

func (r *RangeData) View() RangeDataView {
	return RangeDataView{
		Extent: r.Extent,
		r:      r,
		start:  0,
		end:    r.ByteSize(),
	}
}

func (r RangeDataView) SubRange(ext Extent) (RangeDataView, bool) {
	ext, ok := r.Clamp(ext)
	if !ok {
		return RangeDataView{}, false
	}

	byteOffset := r.start + int((ext.LBA-r.LBA)*BlockSize)
	byteEnd := byteOffset + int(ext.Blocks*BlockSize)

	return RangeDataView{
		Extent: ext,
		r:      r.r,
		start:  int(byteOffset),
		end:    int(byteEnd),
	}, true
}

func (v RangeDataView) EmptyP() bool {
	return v.r.EmptyP()
}

func (v RangeDataView) WriteData() []byte {
	b := v.r.WriteData()

	return b[v.start:v.end]
}

func (v RangeDataView) ReadData() []byte {
	b := v.r.ReadData()

	return b[v.start:v.end]
}

func (v RangeDataView) ByteSize() int {
	return v.end - v.start
}

func (d RangeDataView) Copy(s RangeDataView) int {
	// if the source is empty, it's a noop.
	if s.r.data == nil {
		return d.end - d.start
	}

	return copy(d.WriteData(), s.ReadData())
}

func (r RangeData) Append(o RangeData) RangeData {
	if r.Blocks == 0 {
		return o
	}

	r.dirty = true

	r.data = append(r.data, o.data...)
	r.Blocks += o.Blocks
	return r
}

func (r RangeData) Reader() io.Reader {
	return bytes.NewReader(r.data)
}
