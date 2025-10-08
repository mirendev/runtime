package lsvd

import (
	"log/slog"
	"slices"
	"sort"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/lab47/mode"
	"github.com/oklog/ulid/v2"
)

type Segments struct {
	segmentsMu sync.Mutex
	segments   map[SegmentId]*Segment
}

func NewSegments() *Segments {
	return &Segments{
		segments: make(map[SegmentId]*Segment),
	}
}

func (s *Segments) SegmentIds() []SegmentId {
	var ret []SegmentId

	for k := range s.segments {
		ret = append(ret, k)
	}

	return ret
}

func (s *Segments) LiveSegments() []SegmentId {
	var ret []SegmentId

	for k, s := range s.segments {
		if s.deleted {
			continue
		}
		ret = append(ret, k)
	}

	return ret

}

func (s *Segments) SegmentBlocks(seg SegmentId) (uint64, uint64) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	stats, ok := s.segments[seg]
	if !ok {
		return 0, 0
	}

	return stats.Size, stats.Used
}

// SegmentInfo returns size, used blocks, and extent count for a segment
func (s *Segments) SegmentInfo(seg SegmentId) (uint64, uint64, int) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	stats, ok := s.segments[seg]
	if !ok {
		return 0, 0, 0
	}

	return stats.Size, stats.Used, stats.Extents
}

func (s *Segments) TotalBytes() uint64 {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var size uint64

	for _, s := range s.segments {
		if s.deleted {
			continue
		}
		size += s.Size
	}

	return size * BlockSize
}

func (s *Segments) TotalBlocks() uint64 {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var size uint64

	for _, s := range s.segments {
		if s.deleted {
			continue
		}
		size += s.Size
	}

	return size
}

func (s *Segments) UsedBlocks() uint64 {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var used uint64

	for id, seg := range s.segments {
		if seg.deleted {
			continue
		}
		if seg.Used > 0 {
			// Debug: log segments with non-zero Used
			slog.Default().Debug("UsedBlocks: segment has used blocks",
				"segment", id,
				"used", seg.Used,
				"size", seg.Size)
		}
		used += seg.Used
	}

	return used
}

func (s *Segments) Usage() float64 {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var used, size uint64

	for _, s := range s.segments {
		if s.deleted {
			continue
		}

		used += s.Used
		size += s.Size
	}

	return 100.0 * (float64(used) / float64(size)) // report as a percent
}

// Clear removes all segments
func (s *Segments) Clear() {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	s.segments = make(map[SegmentId]*Segment)
}

func (s *Segments) Create(segId SegmentId, stats *SegmentStats) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	s.segments[segId] = &Segment{
		Size: stats.Blocks,
		Used: stats.Blocks,
	}
}

// CreateWithExtents creates a segment with a specific extent count
func (s *Segments) CreateWithExtents(segId SegmentId, stats *SegmentStats, extents int) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	s.segments[segId] = &Segment{
		Size:    stats.Blocks,
		Used:    stats.Blocks,
		Extents: extents,
	}
}

func (s *Segments) SetSegment(segId SegmentId, total, used uint64) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	s.segments[segId] = &Segment{
		Size: total,
		Used: used,
	}
}

func (s *Segments) IncrementSegment(segId SegmentId, blocks uint64) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	seg, ok := s.segments[segId]
	if !ok {
		s.segments[segId] = &Segment{
			Size:    blocks,
			Used:    blocks,
			Extents: 1,
		}
	} else {
		seg.Size += blocks
		seg.Used += blocks
		seg.Extents++
	}
}

func (s *Segments) CreateOrUpdate(segId SegmentId, usedBlocks uint64) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	// TODO where is the Size coming from in the create case??
	seg, ok := s.segments[segId]
	if ok {
		seg.Used += usedBlocks
	} else {
		s.segments[segId] = &Segment{
			Used: usedBlocks,
		}
	}
}

func (s *Segments) UpdateUsage(log *slog.Logger, self SegmentId, affected []PartialExtent) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	warnedSegments := map[SegmentId]struct{}{}

	for _, r := range affected {
		rng := r.Live

		if seg, ok := s.segments[r.Segment]; ok {
			if seg.deleted {
				continue
			}

			// If we've affected ourselves, that's fine, but we can't
			// run the detectedCleared logic because it will misfire since
			// we might have the same extent written multiple times in the same
			// segment, that's totally valid.
			if r.Segment != self && mode.Debug() {
				if o, ok := seg.detectedCleared(rng); ok {
					log.Warn("detected clearing overlapping extent", "orig", o, "cur", r)
				}
				seg.cleared = append(seg.cleared, rng)
			}

			seg.Used -= uint64(rng.Blocks)
		} else if self != r.Segment {
			if _, seen := warnedSegments[r.Segment]; !seen {
				log.Warn("missing segment during usage update", "id", r.Segment.String())
				warnedSegments[r.Segment] = struct{}{}
			}
		}
	}
}

func (s *Segments) LogSegmentInfo(log hclog.Logger) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	type ent struct {
		seg     SegmentId
		density float64
		stats   *Segment
	}

	var entries []ent

	for segId, stats := range s.segments {
		entries = append(entries, ent{segId, stats.Density(), stats})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].density < entries[j].density
	})

	for _, e := range entries {
		log.Info("segment density", "segment", e.seg,
			"density", e.density,
			"total", e.stats.Size,
			"used", e.stats.Used,
		)
	}
}

func (s *Segments) FindSmallSegments(cutoff, max uint64) []SegmentId {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var (
		used uint64
		ret  []SegmentId
	)

	for id, s := range s.segments {
		if s.deleted {
			continue
		}

		if s.Used <= cutoff {
			used += s.Used
			if used > max {
				break
			}

			max += used

			ret = append(ret, id)
		}
	}

	return ret
}

func (s *Segments) PruneDeadSegments() (int, float64) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var used, size uint64
	var dead int

	for _, s := range s.segments {
		if s.deleted {
			continue
		}

		if s.Used == 0 {
			dead++
			s.deleted = true
			continue
		}

		used += s.Used
		size += s.Size
	}

	return dead, 100.0 * (float64(used) / float64(size)) // report as a percent
}

func (s *Segments) SetDeleted(segId SegmentId, log *slog.Logger) {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	seg, ok := s.segments[segId]
	if ok {
		seg.deleted = true
	} else {
		log.Warn("missing segment to set deleted", "seg", segId)
	}
}

func (s *Segments) FindDeleted() []SegmentId {
	s.segmentsMu.Lock()
	defer s.segmentsMu.Unlock()

	var toDelete []SegmentId

	for i, s := range s.segments {
		if s.deleted {
			toDelete = append(toDelete, i)
		}
	}

	for _, i := range toDelete {
		delete(s.segments, i)
	}

	return toDelete
}

func (d *Segments) AllDeadSegments() ([]SegmentId, error) {
	d.segmentsMu.Lock()
	defer d.segmentsMu.Unlock()

	var ret []SegmentId

	for segId, stats := range d.segments {
		if stats.deleted {
			continue
		}

		if stats.Used == 0 {
			ret = append(ret, segId)
		}
	}

	return ret, nil
}

func (d *Segments) sortedSegments() []SegmentId {
	var ret []SegmentId

	for segId := range d.segments {
		ret = append(ret, segId)
	}

	slices.SortFunc(ret, func(a, b SegmentId) int {
		return ulid.ULID(a).Compare(ulid.ULID(b))
	})

	return ret
}

func (d *Segments) LeastDenseSegment(log *slog.Logger) (SegmentId, uint64, bool, error) {
	d.segmentsMu.Lock()
	defer d.segmentsMu.Unlock()

	var (
		smallestId    SegmentId
		smallestStats *Segment
	)

	for _, segId := range d.sortedSegments() {
		stats := d.segments[segId]

		if stats.deleted {
			continue
		}

		d := stats.Density()

		if smallestStats == nil || d < smallestStats.Density() {
			smallestStats = stats
			smallestId = segId
		}
	}

	if smallestStats == nil {
		return SegmentId{}, 0, false, nil
	}

	return smallestId, smallestStats.Used, true, nil
}

func (d *Segments) PickSegmentToGC(log *slog.Logger, min float64, skip []SegmentId) (SegmentId, bool, error) {
	d.segmentsMu.Lock()
	defer d.segmentsMu.Unlock()

	var (
		smallestId    SegmentId
		smallestStats *Segment
	)

	for _, segId := range d.sortedSegments() {
		stats := d.segments[segId]

		if stats.deleted {
			continue
		}

		if slices.Contains(skip, segId) {
			continue
		}

		d := stats.Density()

		trace(log, "segment density", "segment", segId, "density", d)

		if d > min {
			continue
		}

		if smallestStats == nil || d < smallestStats.Density() {
			smallestStats = stats
			smallestId = segId
		}
	}

	if smallestStats == nil {
		return SegmentId{}, false, nil
	}

	return smallestId, true, nil
}
