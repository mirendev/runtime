package lsvd

import (
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
)

type segmentLayoutData struct {
	_       struct{}               `cbor:",toarray"`
	Extents []ExternalExtentHeader `cbor:"0,keyasint,omitempty" json:"extents,omitempty"`
}

type SegmentLayout struct {
	data segmentLayoutData
}

func (v *SegmentLayout) HasExtents() bool {
	return true
}

func (v *SegmentLayout) Extents() []ExternalExtentHeader {
	return v.data.Extents
}

func (v *SegmentLayout) SetExtents(extents []ExternalExtentHeader) {
	x := slices.Clone(extents)
	v.data.Extents = x
}

func (v *SegmentLayout) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SegmentLayout) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SegmentLayout) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SegmentLayout) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type externalExtentHeaderData struct {
	_       struct{} `cbor:",toarray"`
	Lba     uint64   `cbor:"0,keyasint,omitempty" json:"lba,omitempty"`
	Blocks  uint32   `cbor:"1,keyasint,omitempty" json:"blocks,omitempty"`
	Size    uint32   `cbor:"2,keyasint,omitempty" json:"size,omitempty"`
	Offset  uint32   `cbor:"3,keyasint,omitempty" json:"offset,omitempty"`
	RawSize uint32   `cbor:"4,keyasint,omitempty" json:"raw_size,omitempty"`
}

type ExternalExtentHeader struct {
	data externalExtentHeaderData
}

func (v *ExternalExtentHeader) HasLba() bool {
	return true
}

func (v *ExternalExtentHeader) Lba() uint64 {
	return v.data.Lba
}

func (v *ExternalExtentHeader) SetLba(lba uint64) {
	v.data.Lba = lba
}

func (v *ExternalExtentHeader) HasBlocks() bool {
	return true
}

func (v *ExternalExtentHeader) Blocks() uint32 {
	return v.data.Blocks
}

func (v *ExternalExtentHeader) SetBlocks(blocks uint32) {
	v.data.Blocks = blocks
}

func (v *ExternalExtentHeader) HasSize() bool {
	return true
}

func (v *ExternalExtentHeader) Size() uint32 {
	return v.data.Size
}

func (v *ExternalExtentHeader) SetSize(size uint32) {
	v.data.Size = size
}

func (v *ExternalExtentHeader) HasOffset() bool {
	return true
}

func (v *ExternalExtentHeader) Offset() uint32 {
	return v.data.Offset
}

func (v *ExternalExtentHeader) SetOffset(offset uint32) {
	v.data.Offset = offset
}

func (v *ExternalExtentHeader) HasRawSize() bool {
	return true
}

func (v *ExternalExtentHeader) RawSize() uint32 {
	return v.data.RawSize
}

func (v *ExternalExtentHeader) SetRawSize(raw_size uint32) {
	v.data.RawSize = raw_size
}

func (v *ExternalExtentHeader) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ExternalExtentHeader) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ExternalExtentHeader) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ExternalExtentHeader) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}
