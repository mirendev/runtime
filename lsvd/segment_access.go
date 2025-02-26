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
}

type VolumeInfo struct {
	Name   string      `json:"name"`
	Size   units.Bytes `json:"size"`
	Parent string      `json:"parent"`
	UUID   string      `json:"uuid"`
}

type SegmentAccess interface {
	InitContainer(ctx context.Context) error
	InitVolume(ctx context.Context, vol *VolumeInfo) error
	ListVolumes(ctx context.Context) ([]string, error)
	GetVolumeInfo(ctx context.Context, vol string) (*VolumeInfo, error)

	ListSegments(ctx context.Context, vol string) ([]SegmentId, error)
	OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error)
	WriteSegment(ctx context.Context, seg SegmentId) (io.WriteCloser, error)
	UploadSegment(ctx context.Context, seg SegmentId, f *os.File) error

	RemoveSegment(ctx context.Context, seg SegmentId) error
	RemoveSegmentFromVolume(ctx context.Context, vol string, seg SegmentId) error
	WriteMetadata(ctx context.Context, vol, name string) (io.WriteCloser, error)
	ReadMetadata(ctx context.Context, vol, name string) (io.ReadCloser, error)

	AppendToSegments(ctx context.Context, volume string, seg SegmentId) error
}
