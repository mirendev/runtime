package lsvd

import (
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestSegmentsIncrementSegment(t *testing.T) {
	t.Run("creates segment if not exists", func(t *testing.T) {
		r := require.New(t)
		s := NewSegments()

		segId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))
		s.IncrementSegment(segId, 100)

		total, used := s.SegmentBlocks(segId)
		r.Equal(uint64(100), total)
		r.Equal(uint64(100), used)
	})

	t.Run("increments existing segment", func(t *testing.T) {
		r := require.New(t)
		s := NewSegments()

		segId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))
		s.IncrementSegment(segId, 100)
		s.IncrementSegment(segId, 50)

		total, used := s.SegmentBlocks(segId)
		r.Equal(uint64(150), total)
		r.Equal(uint64(150), used)
	})

	t.Run("incremental tracking with UpdateUsage", func(t *testing.T) {
		r := require.New(t)
		s := NewSegments()
		log := slog.Default()

		segId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))

		// Simulate rebuilding: increment for each extent, then UpdateUsage for overlaps
		s.IncrementSegment(segId, 100) // First extent: 100 blocks
		s.IncrementSegment(segId, 100) // Second extent: 100 blocks (overlaps 30 blocks)

		// Simulate UpdateUsage decrementing for the 30 overlapping blocks
		affected := []PartialExtent{
			{
				Live: Extent{LBA: 70, Blocks: 30},
				ExtentLocation: ExtentLocation{
					ExtentHeader: ExtentHeader{Extent: Extent{LBA: 70, Blocks: 30}},
					Segment:      segId,
				},
			},
		}
		s.UpdateUsage(log, segId, affected)

		total, used := s.SegmentBlocks(segId)
		r.Equal(uint64(200), total, "Total should be sum of all extent blocks")
		r.Equal(uint64(170), used, "Used should be total minus garbage (30)")
	})
}

func TestSegmentsUpdateUsage(t *testing.T) {
	log := slog.Default()

	t.Run("decrements usage when segment has internal overlaps", func(t *testing.T) {
		r := require.New(t)

		s := NewSegments()

		// Create a segment with 1000 blocks
		segId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))
		s.Create(segId, &SegmentStats{Blocks: 1000})

		// Verify initial state
		total, used := s.SegmentBlocks(segId)
		r.Equal(uint64(1000), total)
		r.Equal(uint64(1000), used)

		// Simulate the case where the same segment has overlapping extents
		// E.g., extent 1 covers LBA 100-149 (50 blocks)
		//       extent 2 covers LBA 120-169 (50 blocks)
		// The overlap is LBA 120-149 (30 blocks) which are wasted/garbage
		affected := []PartialExtent{
			{
				Live: Extent{LBA: 100, Blocks: 30}, // The overlapping portion
				ExtentLocation: ExtentLocation{
					ExtentHeader: ExtentHeader{
						Extent: Extent{LBA: 100, Blocks: 30},
					},
					Segment: segId, // Same segment affecting itself
				},
			},
		}

		// Update usage with self-affecting extent
		s.UpdateUsage(log, segId, affected)

		// Used count SHOULD be decremented for internal overlaps (garbage blocks)
		_, used = s.SegmentBlocks(segId)
		r.Equal(uint64(970), used, "Used count should decrease by 30 for internal garbage")
	})

	t.Run("decrements usage when affecting other segments", func(t *testing.T) {
		r := require.New(t)

		s := NewSegments()

		// Create two segments
		oldSegId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))
		newSegId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))

		s.Create(oldSegId, &SegmentStats{Blocks: 1000})
		s.Create(newSegId, &SegmentStats{Blocks: 500})

		// Verify initial state
		_, oldUsed := s.SegmentBlocks(oldSegId)
		_, newUsed := s.SegmentBlocks(newSegId)
		r.Equal(uint64(1000), oldUsed)
		r.Equal(uint64(500), newUsed)

		// Simulate new segment overwriting 100 blocks from old segment
		affected := []PartialExtent{
			{
				Live: Extent{LBA: 100, Blocks: 100},
				ExtentLocation: ExtentLocation{
					ExtentHeader: ExtentHeader{
						Extent: Extent{LBA: 100, Blocks: 100},
					},
					Segment: oldSegId, // Different segment
				},
			},
		}

		// Update usage with extent from old segment
		s.UpdateUsage(log, newSegId, affected)

		// Old segment's used count should be decremented
		_, oldUsed = s.SegmentBlocks(oldSegId)
		r.Equal(uint64(900), oldUsed, "Old segment's used count should decrease by 100")

		// New segment's used count should remain unchanged
		_, newUsed = s.SegmentBlocks(newSegId)
		r.Equal(uint64(500), newUsed, "New segment's used count should not change")
	})

	t.Run("handles multiple overlapping extents in same segment", func(t *testing.T) {
		r := require.New(t)

		s := NewSegments()

		segId := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))
		s.Create(segId, &SegmentStats{Blocks: 1000})

		// Simulate multiple overlapping extents within the same segment
		// Each represents garbage blocks from earlier extents in the same segment
		affected := []PartialExtent{
			{
				Live: Extent{LBA: 100, Blocks: 50},
				ExtentLocation: ExtentLocation{
					ExtentHeader: ExtentHeader{Extent: Extent{LBA: 100, Blocks: 50}},
					Segment:      segId,
				},
			},
			{
				Live: Extent{LBA: 120, Blocks: 80},
				ExtentLocation: ExtentLocation{
					ExtentHeader: ExtentHeader{Extent: Extent{LBA: 120, Blocks: 80}},
					Segment:      segId,
				},
			},
		}

		s.UpdateUsage(log, segId, affected)

		// All overlapping blocks should be counted as garbage
		_, used := s.SegmentBlocks(segId)
		r.Equal(uint64(870), used, "Used should decrease by 50+80=130 for internal garbage")
	})
}
