package lsvd

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"github.com/pierrec/lz4/v4"
	"github.com/pkg/errors"
)

func (d *Disk) GCOnce(ctx *Context) (SegmentId, error) {
	done := make(chan EventResult)

	d.controller.EventsCh() <- Event{
		Kind: StartGC,
		Done: done,
	}

	select {
	case <-ctx.Done():
		return SegmentId{}, ctx.Err()
	case er := <-done:
		return er.Segment, nil
	}
}

type gcExtent struct {
	CE      *compactPE
	Live    Extent
	Segment uint32
}

type CopyIterator struct {
	seg SegmentId
	d   *Disk
	or  SegmentReader
	//br  *bufio.Reader

	err error

	//hdr SegmentHeader

	//left uint32

	expectedBlocks uint64
	originalTotal  uint64
	//totalBlocks    uint64
	copiedExtents int
	copiedBlocks  uint64

	newSegment SegmentId
	builder    *SegmentBuilder

	errorPatching bool

	segmentsProcessed []SegmentId
	extents           []gcExtent
	processedExtents  []gcExtent
	results           []ExtentHeader
}

func (c *CopyIterator) gatherExtents() {
	total, used := c.d.s.SegmentBlocks(c.seg)
	c.expectedBlocks = used
	c.originalTotal = total

	c.segmentsProcessed = append(c.segmentsProcessed, c.seg)
	c.extents = c.extents[:0]

	idx := c.d.lba2pba.segmentIdx(ExtentLocation{
		Segment: c.seg,
		Disk:    0,
	})

	for i := c.d.lba2pba.LockedIterator(); i.Valid(); i.Next() {
		pe := i.CompactValuePtr()
		if pe.segIdx == idx {
			c.extents = append(c.extents, gcExtent{
				CE:      pe,
				Live:    pe.Live(),
				Segment: idx,
			})
		}
	}
}

func (d *CopyIterator) fetchExtent(
	ctx *Context,
	_ *slog.Logger,
	addr ExtentLocation,
) (RangeData, error) {
	startFetch := time.Now()

	rawData := ctx.Allocate(int(addr.Size))

	_, err := d.or.ReadAt(rawData, int64(addr.Offset))
	if err != nil {
		return RangeData{}, err
	}

	var rangeData []byte

	switch addr.Flags() {
	case Uncompressed:
		rangeData = rawData
	case Compressed:
		startDecomp := time.Now()
		sz := addr.RawSize

		uncomp := ctx.Allocate(int(sz))

		n, err := lz4.UncompressBlock(rawData, uncomp)
		if err != nil {
			return RangeData{}, errors.Wrapf(err, "error uncompressing data (rawsize: %d, compdata: %d)", len(rawData), len(uncomp))
		}

		if n != int(sz) {
			return RangeData{}, fmt.Errorf("failed to uncompress correctly, %d != %d", n, sz)
		}

		rangeData = uncomp
		compressionOverhead.Add(time.Since(startDecomp).Seconds())
	default:
		return RangeData{}, fmt.Errorf("unknown flags value: %d", addr.Flags())
	}

	src := MapRangeData(addr.Extent, rangeData)

	readProcessing.Add(time.Since(startFetch).Seconds())
	return src, nil
}

func (c *CopyIterator) ProcessFromExtents(ctx *Context, log *slog.Logger) error {
	log.Debug("copying extents for segment", "extents", len(c.extents), "segment", c.seg, "new-segment", c.newSegment)

	marker := ctx.Marker()

	for _, ce := range c.extents {
		ctx.ResetTo(marker)

		rng := c.d.lba2pba.ToPE(*ce.CE)

		if rng.Size == 0 {
			c.builder.ZeroBlocks(rng.Live)
			c.results = append(c.results, rng.ExtentHeader)

			c.copiedBlocks += uint64(rng.Blocks)
			c.copiedExtents++
			continue
		}

		data, err := c.fetchExtent(ctx, c.d.log, rng.ExtentLocation)
		if err != nil {
			return err
		}

		// Collapse data down to the actual live range

		view, ok := data.SubRange(rng.Live)
		if !ok {
			return fmt.Errorf("error calculating sub-range from %s to %s", rng.Extent, rng.Live)
		}

		_, eh, err := c.builder.WriteExtent(c.d.log, view)
		if err != nil {
			return err
		}

		c.copiedBlocks += uint64(eh.Blocks)
		c.copiedExtents++

		c.results = append(c.results, eh)
	}

	c.processedExtents = append(c.processedExtents, c.extents...)

	return nil
}

func (ci *CopyIterator) extentsByteSize() int {
	var bs int

	for _, e := range ci.extents {
		bs += int(e.CE.byteSize)
	}

	return bs
}

func (c *CopyIterator) updateDisk(ctx context.Context) error {
	trace(c.d.log, "uploading post-gc segment", "segment", c.newSegment)
	var (
		stats *SegmentStats
		err   error
	)

	for {
		_, stats, err = c.builder.Flush(ctx, c.d.log, c.d.volume, c.newSegment, c.d.volName)
		if err != nil {
			c.d.log.Error("error flushing data to segment, retrying", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	trace(c.d.log, "patching block map from post-gc segment", "segment", c.newSegment)
	c.d.s.Create(c.newSegment, stats)

	newIdx := c.d.lba2pba.segmentIdx(ExtentLocation{
		Segment: c.newSegment,
		Disk:    0,
	})

	return c.d.lba2pba.LockToPatch(func() error {
		for i, pe := range c.processedExtents {
			// it's possible that the extent has been deleted and reused by the time
			// we're returning here. So if the segment is different, then we know it's been
			// reused. It's not possible to reuse an extent in the same segment because of how
			// we manage extents in segments.
			if pe.CE.segIdx != pe.Segment {
				c.errorPatching = true
				c.d.log.Warn("unable to patch segment, detected recycled compactExtent")
				continue
			}

			// Double check that we're patching for the same live extent. Otherwise bail!
			if pe.CE.Live() != pe.Live {
				c.errorPatching = true
				c.d.log.Warn("unable to patch segment, detected live range has changed")
				continue
			}

			eh := c.results[i]
			if eh.Size != 0 {
				eh.Offset += stats.DataOffset
			}

			pe.CE.SetFromHeader(eh, newIdx)
		}

		return nil
	})
}

func (c *CopyIterator) Close(ctx context.Context) error {
	err := c.updateDisk(ctx)
	if err != nil {
		return err
	}

	if !c.errorPatching {
		for _, seg := range c.segmentsProcessed {
			c.d.s.SetDeleted(seg, c.d.log)
		}
	}

	c.d.log.Info("gc cycle complete",
		"segments", len(c.segmentsProcessed),
		"new-segment", c.newSegment,
		"extents", c.copiedExtents,
		"blocks", c.copiedBlocks,
		"expected-blocks", c.expectedBlocks,
		"original-blocks", c.originalTotal,
		//"percent", float64(c.copiedBlocks)/float64(c.hdr.ExtentCount),
	)

	c.builder.Close(c.d.log)

	return c.or.Close()
}

func (ci *CopyIterator) Reset(ctx context.Context, seg SegmentId) error {
	ci.seg = seg
	ci.gatherExtents()

	if ci.expectedBlocks == 0 {
		ci.d.log.Info("detected segment completely unused, deleting without GC", "segment", seg)

		ci.d.s.SetDeleted(seg, ci.d.log)

		return nil
	}

	if !ci.newSegment.Valid() {
		newSeg, err := ci.d.nextSeq()
		if err != nil {
			return err
		}

		ci.newSegment = newSeg
	}

	if ci.builder.em == nil {
		ci.builder.em = NewExtentMap()
	}

	if !ci.builder.OpenP() {
		path := filepath.Join(ci.d.path, "writecache."+ci.newSegment.String())
		err := ci.builder.OpenWrite(path, ci.d.log)
		if err != nil {
			return err
		}
	}

	f, err := ci.d.volume.OpenSegment(ctx, seg)
	if err != nil {
		return errors.Wrapf(err, "opening segment %s", seg)
	}

	/*
		br := bufio.NewReader(ToReader(f))

		err = ci.hdr.Read(br)
		if err != nil {
			return err
		}
	*/

	ci.or = f
	//ci.br = br

	//ci.totalBlocks += uint64(ci.hdr.ExtentCount)
	//ci.left += ci.hdr.ExtentCount

	return nil
}

func (d *Disk) CopyIterator(ctx context.Context, seg SegmentId) (*CopyIterator, error) {
	ci := &CopyIterator{
		d:       d,
		seg:     seg,
		builder: NewSegmentBuilder(),
	}

	err := ci.Reset(ctx, seg)
	if err != nil {
		return nil, err
	}

	return ci, nil

}

func (d *Disk) removeSegmentIfPossible(ctx context.Context, seg SegmentId) error {
	volumes, err := d.sa.ListVolumes(ctx)
	if err != nil {
		return err
	}

	for _, vol := range volumes {
		vo, err := d.sa.OpenVolume(ctx, vol)
		if err != nil {
			return err
		}

		segments, err := vo.ListSegments(ctx)
		if err != nil {
			return err
		}

		if slices.Index(segments, seg) != -1 {
			// ok, someone holding on to it, return early
			return nil
		}
	}

	d.log.Info("removing segment", "segment", seg)
	// ok, no volume has it, we can remove it.
	err = d.sa.RemoveSegment(ctx, seg)
	if err != nil {
		return errors.Wrapf(err, "removing segment: %s", seg)
	}

	return nil
}
