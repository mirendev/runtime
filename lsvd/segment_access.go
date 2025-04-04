package lsvd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
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

func (vol *VolumeInfo) Normalize() error {
	if vol.Size == 0 {
		vol.Size = units.GigaBytes(100).Bytes()
	} else if vol.Size < units.MegaBytes(1).Bytes() {
		return fmt.Errorf("volume size %d is too small", vol.Size)
	}

	if vol.UUID == "" {
		u, err := uuid.NewV7()
		if err != nil {
			return err
		}

		vol.UUID = u.String()
	}

	return nil
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

func ReplicaWriter(primary, replica SegmentAccess) SegmentAccess {
	return &replicaWriter{
		primary: primary,
		replica: replica,
	}
}

type replicaWriter struct {
	primary SegmentAccess
	replica SegmentAccess
}

func (t *replicaWriter) InitContainer(ctx context.Context) error {
	if err := t.primary.InitContainer(ctx); err != nil {
		return err
	}
	return t.replica.InitContainer(ctx)
}

func (t *replicaWriter) InitVolume(ctx context.Context, vol *VolumeInfo) error {
	err := vol.Normalize()
	if err != nil {
		return err
	}

	if err := t.primary.InitVolume(ctx, vol); err != nil {
		return err
	}
	return t.replica.InitVolume(ctx, vol)
}

func (t *replicaWriter) ListVolumes(ctx context.Context) ([]string, error) {
	return t.primary.ListVolumes(ctx)
}

func (t *replicaWriter) RemoveSegment(ctx context.Context, seg SegmentId) error {
	if err := t.primary.RemoveSegment(ctx, seg); err != nil {
		return err
	}
	return t.replica.RemoveSegment(ctx, seg)
}

func (t *replicaWriter) OpenVolume(ctx context.Context, vol string) (Volume, error) {
	wVolume, err := t.primary.OpenVolume(ctx, vol)
	if err != nil {
		return nil, err
	}

	rVolume, err := t.replica.OpenVolume(ctx, vol)
	if err != nil {
		return nil, err
	}

	return &teeVolume{
		primary: wVolume,
		replica: rVolume,
	}, nil
}

func (t *replicaWriter) GetVolumeInfo(ctx context.Context, vol string) (*VolumeInfo, error) {
	wInfo, err := t.primary.GetVolumeInfo(ctx, vol)
	if err != nil {
		return nil, err
	}

	rInfo, err := t.replica.GetVolumeInfo(ctx, vol)
	if err != nil {
		return nil, err
	}

	if wInfo.Name != rInfo.Name || wInfo.Size != rInfo.Size || wInfo.Parent != rInfo.Parent || wInfo.UUID != rInfo.UUID {
		return nil, os.ErrInvalid
	}

	return wInfo, nil
}

type teeVolume struct {
	primary Volume
	replica Volume
}

func (t *teeVolume) Info(ctx context.Context) (*VolumeInfo, error) {
	wInfo, err := t.primary.Info(ctx)
	if err != nil {
		return nil, err
	}

	rInfo, err := t.replica.Info(ctx)
	if err != nil {
		return nil, err
	}

	if wInfo.Name != rInfo.Name || wInfo.Size != rInfo.Size || wInfo.Parent != rInfo.Parent || wInfo.UUID != rInfo.UUID {
		return nil, os.ErrInvalid
	}

	return wInfo, nil
}

func (t *teeVolume) ListSegments(ctx context.Context) ([]SegmentId, error) {
	return t.primary.ListSegments(ctx)
}

func (t *teeVolume) OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error) {
	return t.primary.OpenSegment(ctx, seg)
}

func (t *teeVolume) NewSegment(ctx context.Context, seg SegmentId, layout *SegmentLayout, data *os.File) error {
	if err := t.primary.NewSegment(ctx, seg, layout, data); err != nil {
		return err
	}
	return t.replica.NewSegment(ctx, seg, layout, data)
}

func (t *teeVolume) RemoveSegment(ctx context.Context, seg SegmentId) error {
	if err := t.primary.RemoveSegment(ctx, seg); err != nil {
		return err
	}
	return t.replica.RemoveSegment(ctx, seg)
}
