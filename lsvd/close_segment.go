package lsvd

import (
	"context"
)

// CloseSegment synchronously closes the current segment, as well as giving
// any background GC process to finish up first.
func (d *Disk) CloseSegment(ctx context.Context) error {
	if d.curOC == nil || d.curOC.EmptyP() {
		err := d.cleanupDeletedSegments(ctx)
		if err != nil {
			d.log.Error("error cleaning up deleted segments", "error", err)
		}
		return nil
	}

	ch, err := d.closeSegmentAsync(ctx)
	if ch == nil || err != nil {
		return err
	}

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Disk) finalizeSegment(gctx context.Context) error {
	if d.curOC == nil {
		return nil
	}

	if d.curOC.EmptyP() {
		return d.curOC.Close()
	}

	d.log.Info("flushing last segment to storage", "segment", d.curSeq)

	done := make(chan EventResult)
	select {
	case <-gctx.Done():
		return gctx.Err()
	case d.controller.EventsCh() <- Event{
		Kind:      CloseSegment,
		Value:     d.curOC,
		SegmentId: d.curSeq,
		Done:      done,
	}:
		// ok
	}

	select {
	case <-gctx.Done():
		return gctx.Err()
	case <-done:
		return nil
	}
}

func (d *Disk) closeSegmentAsync(gctx context.Context) (chan EventResult, error) {
	segId := d.curSeq

	//s := time.Now()
	oc := d.curOC

	var err error
	d.curOC, err = d.newSegmentCreator()
	if err != nil {
		return nil, err
	}

	d.log.Info("flushing segment to storage in background", "segment", segId)

	d.prevCache.SetWhenClear(oc)

	done := make(chan EventResult, 1)

	select {
	case <-gctx.Done():
		return nil, gctx.Err()
	case d.controller.EventsCh() <- Event{
		Kind:      CloseSegment,
		Value:     oc,
		SegmentId: segId,
		Done:      done,
	}:
		// ok
	}

	return done, nil
}

const (
	GCDensityThreshold = 70.0
	GCTotalThreshold   = 1024 * 1024 // 1MB
)

func (d *Disk) cleanupDeletedSegments(ctx context.Context) error {
	d.deleteMu.Lock()
	defer d.deleteMu.Unlock()

	for _, i := range d.s.FindDeleted() {
		d.log.Info("removing segment from volume", "volume", d.volName, "segment", i)
		err := d.sa.RemoveSegmentFromVolume(ctx, d.volName, i)
		if err != nil {
			return err
		}

		err = d.removeSegmentIfPossible(ctx, i)
		if err != nil {
			return err
		}
	}

	return nil
}
