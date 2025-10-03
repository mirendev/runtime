package lsvd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
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

func ReplicaWriter(log *slog.Logger, primary, replica SegmentAccess) SegmentAccess {
	return &replicaWriter{
		log:     log.With("module", "lsvd-access"),
		primary: primary,
		replica: replica,
	}
}

type replicaWriter struct {
	log     *slog.Logger
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
		log:     t.log,
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
	log     *slog.Logger
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

// composeSegmentList merges two segment lists (primary and replica) intelligently:
// - If one is a tail subset of the other, returns the longer list
// - If they have overlapping segments, merges them
// - Otherwise returns the list with the newer last segment
func composeSegmentList(primary, replica []SegmentId) ([]SegmentId, error) {
	// Handle empty lists
	if len(primary) == 0 {
		return replica, nil
	}
	if len(replica) == 0 {
		return primary, nil
	}

	// Check if one list is a tail subset of the other - use the longer one
	if len(primary) < len(replica) {
		replicaTail := replica[len(replica)-len(primary):]
		if slices.Equal(primary, replicaTail) {
			// Primary only has tail segments, use replica's complete list
			return replica, nil
		}
	} else if len(replica) < len(primary) {
		primaryTail := primary[len(primary)-len(replica):]
		if slices.Equal(replica, primaryTail) {
			// Replica only has tail, use primary's complete list
			return primary, nil
		}
	}

	// Check for overlap and merge: replica's tail matches primary's head
	// This handles cases like replica=[1,2,3,4,5,6] and primary=[5,6,7]
	maxOverlap := min(len(primary), len(replica))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		replicaTail := replica[len(replica)-overlap:]
		primaryHead := primary[:overlap]

		if slices.Equal(replicaTail, primaryHead) {
			// Found overlap - merge the lists
			// Take replica up to overlap point + all of primary
			merged := make([]SegmentId, 0, len(replica)-overlap+len(primary))
			merged = append(merged, replica[:len(replica)-overlap]...)
			merged = append(merged, primary...)
			return merged, nil
		}
	}

	// No overlap found - merge and deduplicate to avoid losing segments
	// Create a map to deduplicate
	seen := make(map[SegmentId]bool)
	merged := make([]SegmentId, 0, len(primary)+len(replica))

	for _, seg := range replica {
		if !seen[seg] {
			seen[seg] = true
			merged = append(merged, seg)
		}
	}

	for _, seg := range primary {
		if !seen[seg] {
			seen[seg] = true
			merged = append(merged, seg)
		}
	}

	// Sort by ULID timestamp
	slices.SortFunc(merged, func(a, b SegmentId) int {
		aTime := ulid.ULID(a).Time()
		bTime := ulid.ULID(b).Time()
		if aTime < bTime {
			return -1
		} else if aTime > bTime {
			return 1
		}
		return 0
	})

	return merged, nil
}

func (t *teeVolume) ListSegments(ctx context.Context) ([]SegmentId, error) {
	primarySegs, primaryErr := t.primary.ListSegments(ctx)
	replicaSegs, replicaErr := t.replica.ListSegments(ctx)

	if primaryErr != nil && replicaErr != nil {
		return nil, primaryErr
	}

	if primaryErr != nil {
		return replicaSegs, nil
	}

	if replicaErr != nil {
		return primarySegs, nil
	}

	return composeSegmentList(primarySegs, replicaSegs)
}

func (t *teeVolume) OpenSegment(ctx context.Context, seg SegmentId) (SegmentReader, error) {
	reader, err := t.primary.OpenSegment(ctx, seg)
	if err == nil {
		return reader, nil
	}

	// Primary failed, try replica
	t.log.Warn("segment not found in primary, falling back to replica", "segment", seg.String())
	return t.replica.OpenSegment(ctx, seg)
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

func (t *teeVolume) Reconcile(ctx context.Context) (*ReconcileResult, error) {
	reconciler := NewSegmentReconciler(t.log, t.primary, t.replica)
	return reconciler.Reconcile(ctx)
}
