package lsvd

import (
	"context"
	"io"
	"os"

	"miren.dev/runtime/pkg/units"
)

type SegmentReader interface {
	io.ReaderAt
	io.Closer

	Layout(ctx context.Context) (*SegmentLayout, error)
}

type VolumeInfo struct {
	Name   string      `json:"name"`
	Size   units.Bytes `json:"size"`
	Parent string      `json:"parent"`
	UUID   string      `json:"uuid"`
}

type Volume interface {
	Info(ctx context.Context) (*VolumeInfo, error)
	ListSegments(ctx context.Context) ([]SegmentId, error)
	OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error)
	NewSegment(ctx context.Context, seg SegmentId, layout *SegmentLayout, data *os.File) error
	RemoveSegment(ctx context.Context, seg SegmentId) error
}

type SegmentAccess interface {
	InitContainer(ctx context.Context) error
	InitVolume(ctx context.Context, vol *VolumeInfo) error
	ListVolumes(ctx context.Context) ([]string, error)
	RemoveSegment(ctx context.Context, seg SegmentId) error

	OpenVolume(ctx context.Context, vol string) (Volume, error)
	GetVolumeInfo(ctx context.Context, vol string) (*VolumeInfo, error)
}
