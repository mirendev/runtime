package lsvd

import (
	"context"
	"path/filepath"

	"miren.dev/runtime/pkg/multierror"
)

type Packer struct {
	d *Disk
	m *ExtentMap

	segId SegmentId
}

func (p *Packer) iterateExtents(ctx *Context) error {
	var live RangeData

	sb := NewSegmentBuilder()

	path := filepath.Join(p.d.path, "writecache."+p.segId.String())
	err := sb.OpenWrite(path, p.d.log)
	if err != nil {
		return err
	}

	d := p.d
	marker := ctx.Marker()

	for i := p.m.Iterator(); i.Valid(); i.Next() {

		d.log.Debug("packing extent", "extent", i.Value().Live)
		data, err := d.ReadExtent(ctx, i.Value().Live)
		if err != nil {
			return err
		}

		if live.Blocks == 0 {
			live = data
			continue
		}

		// combine conjoined extents
		if live.Last()+1 == i.Key() {
			live = live.Append(data)

			if live.Blocks >= 100 {
				d.log.Debug("writing packed extent (big)", "extent", live.Extent)
				_, _, err := sb.WriteExtent(d.log, live.View())
				if err != nil {
					return err
				}
				live = RangeData{}
				ctx.ResetTo(marker)
			}
		} else {
			d.log.Debug("writing packed extent (disjoint)", "extent", live.Extent)
			_, _, err := sb.WriteExtent(d.log, live.View())
			if err != nil {
				return err
			}

			live = data
		}

		if sb.ShouldFlush(FlushThreshHold) {
			err = p.flushSegment(ctx, sb)
			if err != nil {
				return err
			}

			sb.Close(p.d.log)

			sb = NewSegmentBuilder()
		}
	}

	if live.Blocks > 0 {
		d.log.Debug("writing packed extent (final)", "extent", live.Extent)
		_, _, err := sb.WriteExtent(d.log, live.View())
		if err != nil {
			return err
		}
	}

	return p.flushSegment(ctx, sb)
}

func (p *Packer) flushSegment(ctx context.Context, sb *SegmentBuilder) error {
	defer sb.Close(p.d.log)

	d := p.d

	sid := p.segId

	d.log.Debug("creating packed segment", "id", sid)

	locs, stats, err := sb.Flush(ctx, d.log, d.volume, sid, d.volName)
	if err != nil {
		return err
	}

	d.s.Create(sid, stats)

	err = p.m.UpdateBatch(d.log, locs, sid, d.s)
	if err != nil {
		return err
	}

	return nil
}

func (p *Packer) Pack(gctx context.Context) error {
	seg, err := p.d.nextSeq()
	if err != nil {
		return err
	}

	p.segId = seg

	for seg, stats := range p.d.s.segments {
		trace(p.d.log, "pre-pack segment", "segment", seg, "used", stats.Used)
	}

	ctx := NewContext(gctx)

	err = p.iterateExtents(ctx)
	if err != nil {
		return err
	}

	err = p.removeOldSegments(gctx)
	for seg, stats := range p.d.s.segments {
		trace(p.d.log, "post-pack segment", "segment", seg, "used", stats.Used)
	}

	return err
}

func (p *Packer) removeOldSegments(ctx context.Context) error {
	segments, err := p.d.s.AllDeadSegments()
	if err != nil {
		return err
	}

	var rerr error

	for _, seg := range segments {
		p.d.log.Debug("removing dead segment", "id", seg)
		err := p.d.removeSegmentIfPossible(ctx, seg)
		if err != nil {
			rerr = multierror.Append(rerr, err)
			continue
		}

		p.d.s.SetDeleted(seg, p.d.log)
	}

	if rerr != nil {
		return rerr
	}

	p.d.log.Debug("removed dead segments", "count", len(segments))

	return nil
}

func (d *Disk) Pack(ctx context.Context) error {
	err := d.CloseSegment(ctx)
	if err != nil {
		return err
	}

	trace(d.log, "beginning pack process")

	packer := &Packer{d: d, m: d.lba2pba}
	return packer.Pack(ctx)
}
