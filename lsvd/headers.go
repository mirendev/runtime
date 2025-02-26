package lsvd

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"
)

type Segment struct {
	Size uint64
	Used uint64

	deleted bool
	cleared []Extent
}

func (s *Segment) detectedCleared(ext Extent) (Extent, bool) {
	for _, x := range s.cleared {
		if ext.Cover(x) != CoverNone {
			return x, true
		}
	}

	return Extent{}, false
}

func (s *Segment) Density() float64 {
	if s.Size == 0 {
		return 0
	}

	return float64(s.Used) / float64(s.Size)
}

func ReadSegmentHeader(path string) (*SegmentHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var hdr SegmentHeader

	err = binary.Read(f, binary.BigEndian, &hdr)
	return &hdr, err
}

type SegmentHeader struct {
	ExtentCount uint32
	DataOffset  uint32
}

func (s SegmentHeader) Write(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, s)
}

func (s *SegmentHeader) Read(r io.Reader) error {
	err := binary.Read(r, binary.BigEndian, &s.ExtentCount)
	if err != nil {
		return err
	}

	err = binary.Read(r, binary.BigEndian, &s.DataOffset)
	if err != nil {
		return err
	}

	return nil
}

const (
	Uncompressed = 0
	Compressed   = 1
	Empty        = 2
)

type ExtentHeader struct {
	Extent `json:"extent" cbor:"1,keyasint"`
	Size   uint32 `json:"size" cbor:"2,keyasint"`
	Offset uint32 `json:"offset" cbor:"3,keyasint"`

	// used when the extent is compressed
	RawSize uint32 `json:"raw_size,omitempty" cbor:"4,keyasint,omitempty"`
}

func (e *ExtentHeader) Flags() byte {
	switch {
	case e.Size == 0:
		return Empty
	case e.RawSize != 0:
		return Compressed
	default:
		return Uncompressed
	}
}

func init() {
	sz := unsafe.Sizeof(ExtentHeader{})
	if sz != 32 {
		panic(fmt.Sprintf("wrong size: %d", sz))
	}
}

func (e *ExtentHeader) Read(r io.ByteReader) (int, error) {
	var size int

	lba, n, err := ReadUvarint(r)
	if err != nil {
		return size, err
	}

	size += n

	e.LBA = LBA(lba)

	blocks, n, err := ReadUvarint(r)
	if err != nil {
		return size, err
	}

	size += n

	e.Blocks = uint32(blocks)

	esz, n, err := ReadUvarint(r)
	if err != nil {
		return size, err
	}

	e.Size = uint32(esz)

	size += n

	off, n, err := ReadUvarint(r)
	if err != nil {
		return size, err
	}

	size += n

	e.Offset = uint32(off)

	rs, n, err := ReadUvarint(r)
	if err != nil {
		return size, err
	}

	size += n

	e.RawSize = uint32(rs)

	return size, nil
}

// PutUvarint encodes a uint64 into buf and returns the number of bytes written.
// If the buffer is too small, PutUvarint will panic.
func WriteUvarint(w io.ByteWriter, x uint64) (int, error) {
	i := 0
	var err error
	for x >= 0x80 {
		err = w.WriteByte(byte(x) | 0x80)
		if err != nil {
			return i, err
		}
		x >>= 7
		i++
	}
	err = w.WriteByte(byte(x))
	return i + 1, err
}

// ReadUvarint reads an encoded unsigned integer from r and returns it as a uint64.
// The error is [io.EOF] only if no bytes were read.
// If an [io.EOF] happens after reading some but not all the bytes,
// ReadUvarint returns [io.ErrUnexpectedEOF].
func ReadUvarint(r io.ByteReader) (uint64, int, error) {
	var x uint64
	var s uint
	for i := 0; i < binary.MaxVarintLen64; i++ {
		b, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return x, i, err
		}
		if b < 0x80 {
			if i == binary.MaxVarintLen64-1 && b > 1 {
				return x, i, io.EOF
			}
			return x | uint64(b)<<s, i + 1, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return x, binary.MaxVarintLen64, io.EOF
}

func (e *ExtentHeader) Write(w io.ByteWriter) (int, error) {
	var sz int

	n, err := WriteUvarint(w, uint64(e.LBA))
	if err != nil {
		return 0, err
	}

	sz += n

	n, err = WriteUvarint(w, uint64(e.Blocks))
	if err != nil {
		return 0, err
	}

	sz += n

	n, err = WriteUvarint(w, uint64(e.Size))
	if err != nil {
		return 0, err
	}

	sz += n

	n, err = WriteUvarint(w, uint64(e.Offset))
	if err != nil {
		return 0, err
	}

	sz += n

	n, err = WriteUvarint(w, uint64(e.RawSize))
	if err != nil {
		return 0, err
	}

	sz += n

	return sz, nil
}
