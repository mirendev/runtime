package lsvd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lab47/mode"
	"github.com/pkg/errors"
)

type EventKind int

const (
	CloseSegment EventKind = iota
	CleanupSegments
	StartGC
	SweepSmallSegments
	ImproveDensity
)

type Event struct {
	Kind      EventKind
	Value     any
	SegmentId SegmentId
	Done      chan EventResult
}

type EventResult struct {
	Segment SegmentId
	Error   error
}

type Controller struct {
	log      *slog.Logger
	d        *Disk
	events   chan Event
	internal []Event

	lastNewSegment time.Time
}

func NewController(ctx context.Context, d *Disk) (*Controller, error) {
	c := &Controller{
		log:    d.log,
		d:      d,
		events: make(chan Event, 20),
	}

	return c, nil
}

func (c *Controller) Run(ctx context.Context) {
	c.handleControl(ctx)
}

func (c *Controller) EventsCh() chan Event {
	return c.events
}

func (c *Controller) queueInternal(ev Event) {
	c.internal = append(c.internal, ev)
}

func (c *Controller) handleControl(gctx context.Context) {
	ctx := NewContext(gctx)

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	for {
		for _, ev := range c.internal {
			ctx.Reset()
			err := c.handleEvent(ctx, ev)
			if err != nil {
				c.log.Error("error handling event", "error", err, "event-kind", ev.Kind)
			}
		}

		c.internal = c.internal[:0]

		ctx.Reset()
		select {
		case <-ctx.Done():
			c.log.Debug("controller loop exitting, context done")
			return
		case ev, ok := <-c.events:
			if !ok {
				c.log.Debug("controller loop exitting, closed")
				return
			}

			err := c.handleEvent(ctx, ev)
			if err != nil {
				c.log.Error("error handling event", "error", err, "event-kind", ev.Kind)
			}
		case <-tick.C:
			err := c.handleTick(ctx)
			if err != nil {
				c.log.Error("error handling tick", "error", err)
			}
		}
	}
}

func (c *Controller) handleTick(ctx *Context) error {
	if time.Since(c.lastNewSegment) >= 5*time.Minute {
		c.lastNewSegment = time.Now()

		err := c.handleLongIdle(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

const (
	MaxBlocksPerSmallPack = 20_000
	SmallSegmentCutOff    = 200
	TargetDensity         = 90
)

func (c *Controller) handleLongIdle(ctx *Context) error {
	smallSegments := c.d.s.FindSmallSegments(SmallSegmentCutOff, MaxBlocksPerSmallPack)
	if len(smallSegments) < 2 {
		return c.improveDensity(ctx)
	}

	c.log.Info("gather small segments for packing cycle", "segments", len(smallSegments))

	return c.packSegments(ctx, Event{}, smallSegments)
}

func (c *Controller) improveDensity(ctx *Context) error {
	toGC, density, ok, err := c.d.s.LeastDenseSegment(c.log)
	if err != nil {
		return err
	}

	if !ok {
		c.log.Warn("GC was requested, but no least dense segment available")
		return nil
	}

	if density >= TargetDensity {
		return nil
	}

	c.log.Info("improving density by GC'ing segment", "segment", toGC, "density", density)

	return c.gcSegment(ctx, Event{}, toGC)
}

func (c *Controller) sweepSmallSegments(ctx *Context, ev Event) error {
	smallSegments := c.d.s.FindSmallSegments(SmallSegmentCutOff, MaxBlocksPerSmallPack)
	if len(smallSegments) < 2 {
		return c.returnError(ev, nil)
	}

	c.log.Info("gather small segments for packing cycle", "segments", len(smallSegments))

	return c.packSegments(ctx, ev, smallSegments)
}

func (c *Controller) handleEvent(ctx *Context, ev Event) error {
	switch ev.Kind {
	case CloseSegment:
		return c.closeSegment(ctx, ev)
	case CleanupSegments:
		return c.returnError(ev, c.d.cleanupDeletedSegments(ctx))
	case StartGC:
		return c.startGC(ctx, ev)
	case SweepSmallSegments:
		return c.sweepSmallSegments(ctx, ev)
	case ImproveDensity:
		return c.returnError(ev, c.improveDensity(ctx))
	default:
		return fmt.Errorf("unknown kind: %d", ev.Kind)
	}
}

func (c *Controller) closeSegment(ctx *Context, ev Event) error {
	oc := ev.Value.(*SegmentCreator)
	done := ev.Done
	segId := ev.SegmentId

	s := time.Now()

	c.lastNewSegment = s

	d := c.d

	defer c.log.Debug("finished goroutine to close segment")
	if done != nil {
		defer func() {
			defer close(done)
			done <- EventResult{
				Segment: segId,
			}
		}()
	}
	defer segmentsWritten.Inc()
	defer oc.Close()

	defer func() {
		segmentTotalTime.Add(time.Since(s).Seconds())
	}()

	var (
		entries []ExtentLocation
		stats   *SegmentStats
		err     error
	)

	// We retry because flush does network calls and we want to just keep trying
	// forever.
	start := time.Now()
	for {
		entries, stats, err = oc.Flush(ctx, d.volume, segId)
		if err != nil {
			c.log.Error("error flushing data to segment, retrying", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		break
	}

	flushDur := time.Since(start)

	c.log.Debug("segment published, resetting write cache")

	var validator *extentValidator

	if false && mode.Debug() {
		validator = &extentValidator{}
		validator.populate(c.log, ctx, d, oc, entries)
	}

	mapStart := time.Now()

	d.s.CreateWithExtents(segId, stats, len(entries))

	err = d.lba2pba.UpdateBatch(c.log, entries, segId, d.s)
	if err != nil {
		c.log.Error("error updating lba map", "error", err)
	}

	extents.Set(float64(d.lba2pba.m.Len()))

	d.prevCache.Clear()

	mapDur := time.Since(mapStart)

	if validator != nil {
		validator.validate(ctx, c.log, d)
	}

	if d.afterNS != nil {
		d.afterNS(segId)
	}

	finDur := time.Since(start)

	c.log.Info("uploaded new segment", "segment", segId, "flush-dur", flushDur, "map-dur", mapDur, "dur", finDur)

	c.queueInternal(Event{
		Kind: CleanupSegments,
	})

	density := d.s.Usage()
	dataDensity.Set(density)

	c.log.Info("finished background segment flush", "total-density", density)

	if d.autoGC && d.s.TotalBytes() > GCTotalThreshold && density < GCDensityThreshold {
		c.log.Info("data density dropped below GC threshold, starting GC",
			"density", density,
			"theshold", GCDensityThreshold,
		)

		c.queueInternal(Event{
			Kind: StartGC,
		})
	}

	return nil
}

func (c *Controller) returnError(ev Event, err error) error {
	if ev.Done != nil {
		go func() {
			defer close(ev.Done)
			ev.Done <- EventResult{
				Error: err,
			}
		}()
	}

	return err
}

func (c *Controller) startGC(ctx *Context, ev Event) error {
	gcCount.Inc()
	s := time.Now()

	defer func() {
		gcTime.Add(time.Since(s).Seconds())
	}()

	d := c.d

	dead, newDensity := c.d.s.PruneDeadSegments()
	if dead > 0 {
		c.queueInternal(Event{
			Kind: CleanupSegments,
		})

		d.log.Info("detected and pruned dead segments", "segments", dead, "new-density", newDensity)
		if newDensity > GCDensityThreshold {
			if ev.Done != nil {
				go func() {
					defer close(ev.Done)
					ev.Done <- EventResult{}
				}()
			}
		}
	}

	if density := d.s.Usage(); density > GCDensityThreshold {
		d.log.Debug("skipping GC has usage has raised since request", "density", density)
		return nil
	}

	toGC, _, ok, err := d.s.LeastDenseSegment(d.log)
	if !ok {
		d.log.Warn("GC was requested, but no least dense segment available")
		return nil
	}

	if err != nil {
		return errors.Wrapf(err, "error picking segment to GC")
	}

	return c.gcSegment(ctx, ev, toGC)
}

func (c *Controller) gcSegment(ctx *Context, ev Event, toGC SegmentId) error {
	d := c.d

	ci, err := d.CopyIterator(ctx, toGC)
	if err != nil {
		d.log.Error("error creating copy iterator segment to GC",
			"error", err,
			"segment", toGC,
		)
		return c.returnError(ev, err)
	}

	if ci == nil {
		d.log.Info("copied found a dead segment and deleted it directly, gc skipped")
	} else {
		d.log.Info("beginning GC of segment", "segment", toGC)

		err = ci.ProcessFromExtents(ctx, d.log)
		if err != nil {
			d.log.Error("error processing segment for gc", "error", err, "segment", toGC)
			return c.returnError(ev, err)
		}

		err = ci.Close(ctx)
		if err != nil {
			d.log.Error("error closing segment after gc", "error", err, "segment", toGC)
			return c.returnError(ev, err)
		}
	}

	density := d.s.Usage()

	d.log.Info("GC cycle complete", "updated-density", density)

	dataDensity.Set(density)

	if ev.Done != nil {
		go func() {
			defer close(ev.Done)
			ev.Done <- EventResult{
				Segment: toGC,
			}
		}()
	}

	c.lastNewSegment = time.Now()

	c.queueInternal(Event{
		Kind: CleanupSegments,
	})

	return nil
}

func (c *Controller) packSegments(ctx *Context, ev Event, segments []SegmentId) error {
	gcCount.Inc()
	s := time.Now()

	defer func() {
		gcTime.Add(time.Since(s).Seconds())
	}()

	d := c.d

	ci := CopyIterator{
		d:       c.d,
		builder: NewSegmentBuilder(),
	}

	for _, toGC := range segments {
		err := ci.Reset(ctx, toGC)
		if err != nil {
			return c.returnError(ev, errors.Wrapf(err, "reseting copy iterator"))
		}

		// Skip processing if segment was completely dead (Reset detected and marked it deleted)
		if ci.expectedBlocks == 0 {
			d.log.Info("skipping GC of dead segment (no live blocks)", "segment", toGC)
			continue
		}

		d.log.Info("beginning GC of segment", "segment", toGC)

		err = ci.ProcessFromExtents(ctx, d.log)
		if err != nil {
			d.log.Error("error processing segment for gc", "error", err, "segment", toGC)
			return c.returnError(ev, err)
		}
	}

	err := ci.Close(ctx)
	if err != nil {
		d.log.Error("error closing segment after gc", "error", err)
		return c.returnError(ev, err)
	}

	density := d.s.Usage()

	d.log.Info("GC cycle complete", "updated-density", density)

	dataDensity.Set(density)

	if ev.Done != nil {
		go func() {
			defer close(ev.Done)
			ev.Done <- EventResult{}
		}()
	}

	c.lastNewSegment = time.Now()

	c.queueInternal(Event{
		Kind: CleanupSegments,
	})

	return nil
}
