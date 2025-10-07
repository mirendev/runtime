package lsvd

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/lab47/mode"

	"github.com/oklog/ulid/v2"
	"github.com/pkg/errors"
)

const (
	// The size of all blocks in bytes
	BlockSize = 4 * 1024

	// How big the segment gets before we flush it to S3
	FlushThreshHold = 32 * 1024 * 1024

	// Maximum time a segment can stay open before being flushed
	MaxSegmentLifetime = 10 * time.Minute
)

type Disk struct {
	SeqGen func() ulid.ULID
	log    *slog.Logger
	path   string

	size     int64
	volume   Volume
	volName  string
	readOnly bool
	useZstd  bool

	prevCache *PreviousCache

	curSeq SegmentId

	lba2pba *ExtentMap
	er      *ExtentReader

	sa    SegmentAccess
	curOC *SegmentCreator

	s *Segments

	afterNS func(SegmentId)

	readDisks []*Disk


	autoGC bool

	deleteMu sync.Mutex

	controller *Controller
	wg         sync.WaitGroup
	closed     bool

	cpsScratch     []CachePosition
	readReqScratch []readRequest
	extentsScratch []Extent
	peScratch      []PartialExtent
}

func NewDisk(ctx context.Context, log *slog.Logger, path string, options ...Option) (*Disk, error) {
	var o opts
	o.autoCreate = true

	for _, opt := range options {
		opt(&o)
	}

	if o.sa == nil {
		o.sa = &LocalFileAccess{Dir: path, Log: log}
	}

	if o.volName == "" {
		o.volName = "default"
	}

	err := o.sa.InitContainer(ctx)
	if err != nil {
		return nil, err
	}

	var sz int64

	vi, err := o.sa.GetVolumeInfo(ctx, o.volName)
	if err != nil || vi.Name == "" {
		if !o.autoCreate {
			return nil, fmt.Errorf("unknown volume: %s", o.volName)
		}

		err = o.sa.InitVolume(ctx, &VolumeInfo{Name: o.volName})
		if err != nil {
			return nil, err
		}
	} else {
		sz = vi.Size.Int64()
	}

	for _, ld := range o.lowers {
		if !ld.readOnly {
			return nil, fmt.Errorf("lower disk not open'd read-only")
		}
	}

	log.Info("attaching to volume", "name", o.volName, "size", sz)

	volume, err := o.sa.OpenVolume(ctx, o.volName)
	if err != nil {
		return nil, err
	}

	er, err := NewExtentReader(log, filepath.Join(path, "readcache"), volume)
	if err != nil {
		return nil, err
	}

	vo, err := o.sa.OpenVolume(ctx, o.volName)
	if err != nil {
		return nil, err
	}

	d := &Disk{
		log:            log,
		path:           path,
		size:           sz,
		volume:         vo,
		lba2pba:        nil, // Will be set by loadLBAMap or rebuildFromSegments
		sa:             o.sa,
		volName:        o.volName,
		SeqGen:         o.seqGen,
		afterNS:        o.afterNS,
		readOnly:       o.ro,
		useZstd:        o.useZstd,
		er:             er,
		prevCache:      NewPreviousCache(),
		s:              NewSegments(),
		cpsScratch:     make([]CachePosition, 0, 1),
		readReqScratch: make([]readRequest, 0, 10),
		extentsScratch: make([]Extent, 0, 10),
		peScratch:      make([]PartialExtent, 0, 10),
	}

	d.readDisks = append(d.readDisks, d)
	d.readDisks = append(d.readDisks, o.lowers...)

	if !d.readOnly {
		err = d.restoreWriteCache(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "restoring write cache")
		}

		if d.curOC == nil {
			d.curOC, err = d.newSegmentCreator()
			if err != nil {
				return nil, errors.Wrapf(err, "creating segment creator")
			}
		}

		log.Info("starting sequence", "seq", d.curSeq)
	}

	cont, err := NewController(ctx, d)
	if err != nil {
		return nil, err
	}

	d.wg.Add(1)

	go func() {
		defer d.wg.Done()
		cont.Run(context.Background())
	}()

	d.controller = cont

	goodMap, err := d.loadLBAMap(ctx)
	if err != nil {
		log.Error("error loading LBA map", "error", err)
		goodMap = false
	}

	if goodMap {
		log.Info("reusing serialized LBA map", "blocks", d.lba2pba.Len())
	} else {
		err = d.rebuildFromSegments(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "rebuilding segments")
		}
	}

	dataDensity.Set(d.s.Usage())

	d.autoGC = o.autoGC

	return d, nil
}

func (r *Disk) SetAfterNS(f func(SegmentId)) {
	r.afterNS = f
}

type ExtentLocation struct {
	ExtentHeader `json:"header" cbor:"1,keyasint"`
	Segment      SegmentId `json:"segment" cbor:"2,keyasint"`
	Disk         uint16    `json:"disk" cbor:"3,keyasint"`
}

type PartialExtent struct {
	Live           Extent `json:"live" cbor:"1,keyasint"`
	ExtentLocation `json:"location" cbor:"2,keyasint"`
}

func (r *PartialExtent) String() string {
	return fmt.Sprintf("%s (%s): %s %d:%d", r.Live, r.Extent, r.Segment, r.Offset, r.Size)
}

func (d *Disk) nextSeq() (SegmentId, error) {
	if d.SeqGen != nil {
		return SegmentId(d.SeqGen()), nil
	}

	ul, err := ulid.New(ulid.Now(), ulid.DefaultEntropy())
	if err != nil {
		return SegmentId{}, err
	}

	return SegmentId(ul), nil
}

func (d *Disk) newSegmentCreator() (*SegmentCreator, error) {
	seq, err := d.nextSeq()
	if err != nil {
		return nil, errors.Wrapf(err, "error generating sequence number")
	}

	d.curSeq = seq

	path := filepath.Join(d.path, "writecache."+seq.String())
	sc, err := NewSegmentCreator(d.log, d.volName, path)
	if err != nil {
		return nil, err
	}

	d.log.Debug("creating new segment creator", "segment", seq, "oc", fmt.Sprintf("%p", sc))
	return sc, nil
}

// Used to test things are setup the way we expect
func (d *Disk) resolveSegmentAccess(ext Extent) ([]PartialExtent, error) {
	return d.lba2pba.Resolve(d.log, ext, nil)
}

func (d *Disk) trace(ctx context.Context, msg string, args ...any) {
	d.log.Log(ctx, LevelTrace, msg, args...)
}

func (d *Disk) ReadExtent(ctx *Context, rng Extent) (RangeData, error) {
	data := NewRangeData(ctx, rng)

	cp, err := d.ReadExtentInto(ctx, data)
	if cp.fd != nil {
		d.trace(ctx, "read extent from cache", "extent", rng, "fd", cp.fd)
		err = FillFromeCache(data.WriteData(), []CachePosition{cp})
		if err != nil {
			return RangeData{}, err
		}
	}

	return data, err
}

type readRequest struct {
	pe     PartialExtent
	extent Extent
	extra  []Extent
}

func (d *Disk) ReadExtentInto(ctx *Context, data RangeData) (CachePosition, error) {
	start := time.Now()

	defer func() {
		blocksReadLatency.Observe(time.Since(start).Seconds())
	}()

	rng := data.Extent

	blocksRead.Add(float64(rng.Blocks))

	iops.Inc()

	log := d.log

	d.trace(ctx, "attempting to fill request from write cache", "extent", rng)

	remaining, err := d.fillFromWriteCache(ctx, log, data)
	if err != nil {
		return CachePosition{}, err
	}

	// Completely filled range from the write cache
	if len(remaining) == 0 {
		d.trace(ctx, "extent filled entirely from write cache")
		return CachePosition{}, nil
	}

	//log.Trace("remaining extents needed", "total", len(remaining))

	var (
		reqs      = d.readReqScratch[:0]
		peScratch = d.peScratch[:0]
		last      *readRequest
	)

	// remaining is the extents that we still need to fill.
	for _, h := range remaining {

		// We resolve each one into a set of partial extents which have
		// information about which segment the partials are in.
		//
		// Invariant: each of the pes.Partial extents must be a part of +h+.
		pes, err := d.lba2pba.Resolve(log, h, peScratch)
		if err != nil {
			log.Error("error computing opbas", "error", err, "rng", h)
			return CachePosition{}, err
		}

		if len(pes) == 0 {
			d.trace(ctx, "no partial extents found")
			if v, ok := data.SubRange(h); ok {
				clear(v.WriteData())
			}
			// nothing for range, and since the data is pre-zero'd, we
			// don't need to clear anything here.
		} else {
			// Pure read from one extent, optimize!
			if len(remaining) == 1 && remaining[0] == rng && len(pes) == 1 && pes[0].Flags() == Uncompressed {
				//log.Trace("reading single, uncompressed extent via fast path")
				// Invariants: remaining[0] == rng == data.Extent
				// Invariants: pes[0].Live fully covers remaining[0]
				pe := pes[0]
				ld := d.readDisks[pe.Disk]
				cps, err := ld.readOneExtent(ctx, &pe, rng, data)
				if err != nil {
					return CachePosition{}, err
				}

				return cps, nil
			}

			for _, pe := range pes {
				if pe.Size == 0 {
					if v, ok := data.SubRange(pe.Live); ok {
						clear(v.WriteData())
					}
					// it's empty! cool cool, we don't need to fill the hole
					// since the slice we're filling inside data has already been
					// cleared when it's created.
					continue
				}

				if mode.Debug() && pe.Live.Cover(h) == CoverNone {
					log.Error("resolve returned extent that doesn't cover", "hole", h, "pe", pe.Live)
				}

				// Because the holes can be smaller than the read ranges,
				// 2 or more holes in sequence might be served by the same
				// segment range.
				if last != nil && last.pe == pe {
					last.extra = append(last.extra, h)
				} else {
					idx := len(reqs)
					reqs = append(reqs, readRequest{
						pe:     pe,
						extent: h,
					})
					last = &reqs[idx]
				}
			}
		}
	}

	if log.Enabled(ctx, LevelTrace) {
		d.trace(ctx, "pes needed", "total", len(reqs))

		for _, o := range reqs {
			d.trace(ctx, "partial-extent needed",
				"segment", o.pe.Segment, "offset", o.pe.Offset, "size", o.pe.Size,
				"usable", o.pe.Live, "full", o.pe.Extent,
				"disk-id", o.pe.Disk,
			)
		}
	}

	// With our set of segments and partial extents in hand, go reach each one
	// and populate data. This could be parallelized as each touches a different
	// range of data.
	for _, o := range reqs {
		ld := d.readDisks[o.pe.Disk]
		extents := d.extentsScratch[:1]
		extents[0] = o.extent

		if o.extra != nil {
			extents = append(extents, o.extra...)
		}

		err := ld.readPartialExtent(ctx, &o.pe, extents, rng, data)
		if err != nil {
			return CachePosition{}, err
		}
	}

	d.readReqScratch = reqs[:0]

	return CachePosition{}, nil
}

func (d *Disk) fillFromWriteCache(ctx *Context, log *slog.Logger, data RangeData) ([]Extent, error) {
	if d.curOC == nil {
		return []Extent{data.Extent}, nil
	}

	used, err := d.curOC.FillExtent(ctx, data.View())
	if err != nil {
		return nil, err
	}

	var remaining []Extent

	/*
		if log.IsTrace() {
			log.Trace("write cache used", "request", data.Extent, "used", used)
		}
	*/

	if len(used) == 0 {
		remaining = []Extent{data.Extent}
	} else {
		var ok bool
		remaining, ok = data.SubMany(used)
		if !ok {
			return nil, fmt.Errorf("internal error calculating remaining extents")
		}
	}

	/*
		if log.IsTrace() {
			log.Trace("requesting reads from prev cache", "used", used, "remaining", remaining)
		}
	*/

	return d.fillingFromPrevWriteCache(ctx, log, data, remaining)
}

func (d *Disk) fillingFromPrevWriteCache(ctx *Context, log *slog.Logger, data RangeData, holes []Extent) ([]Extent, error) {
	oc := d.prevCache.Load()

	// If there is no previous cache, bail.
	if oc == nil {
		return holes, nil
	}

	var remaining []Extent

	for _, sub := range holes {
		sr, ok := data.SubRange(sub)
		if !ok {
			return nil, fmt.Errorf("error calculating subrange")
		}

		used, err := oc.FillExtent(ctx, sr)
		if err != nil {
			return nil, err
		}

		if len(used) == 0 {
			remaining = append(remaining, sub)
		} else {
			res, ok := sub.SubMany(used)
			if !ok {
				return nil, fmt.Errorf("error subtracting partial holes")
			}

			remaining = append(remaining, res...)
		}
	}

	d.trace(ctx, "write cache didn't find", "input", holes, "holes", remaining)

	return remaining, nil
}

func (d *Disk) readOneExtent(
	ctx *Context,
	pe *PartialExtent,
	x Extent,
	dest RangeData,
) (CachePosition, error) {
	src, cps, err := d.er.fetchExtent(ctx, d.log, pe, d.cpsScratch[:0])
	if err != nil {
		return CachePosition{}, err
	}

	if len(cps) == 1 {
		//d.log.Trace("single extent found directly in read cache")
		// There are a few elements, let's write them out so we keep them straight:
		// pe.Extent is the data covered by cps[0]
		// pe.Live is sub-range of pe.Extent that is only the data to consider
		// x is the data the user requests, and it's contained fully within pe.Live

		adjusted := cps[0]

		// go from extent to live
		adjusted.off += (int64(pe.Live.LBA-pe.LBA) * BlockSize)
		adjusted.size = int64(pe.Live.ByteSize())

		// go from live to x
		adjusted.off += (int64(x.LBA-pe.Live.LBA) * BlockSize)
		adjusted.size = int64(x.ByteSize())

		return adjusted, nil
	}

	d.cpsScratch = cps[:0]

	//d.log.Trace("single extent not found in cache", "cps", len(cps))

	inflateCache.Inc()

	rawData := ctx.Allocate(int(pe.Size))

	err = FillFromeCache(rawData, cps)
	if err != nil {
		return CachePosition{}, err
	}

	src = MapRangeData(pe.Extent, rawData)

	// the bytes at the beginning of data are for LBA dataBegin.LBA.
	// the bytes at the beginning of rawData are for LBA full.LBA.
	// we want to compute the 2 byte ranges:
	//   1. the byte range for rng within data
	//   2. the byte range for rng within rawData
	// Then we copy the bytes from 2 to 1.
	overlap, ok := pe.Live.Clamp(x)
	if !ok {
		d.log.Error("error clamping required range to usable range", "request", x, "partial", pe.Live)
		return CachePosition{}, fmt.Errorf("error clamping range")
	}

	d.trace(ctx, "preparing to copy data from segment", "request", x, "clamped", overlap)

	// Compute our source range and destination range against overlap

	subDest, ok := dest.SubRange(overlap)
	if !ok {
		d.log.Error("error clamping range", "full", pe.Live, "sub", overlap)
		return CachePosition{}, fmt.Errorf("error clamping range: %s => %s", pe.Live, overlap)
	}

	subSrc, ok := src.SubRange(overlap)
	if !ok {
		d.log.Error("error calculate source subrange",
			"input", src.Extent, "sub", overlap,
			"request", x, "usable", pe.Live,
			"full", pe.Extent,
		)
		return CachePosition{}, fmt.Errorf("error calculate source subrange")
	}

	if d.log.Enabled(ctx, slog.LevelDebug) {
		d.trace(ctx, "copying segment data",
			"src", src.Extent,
			"dest", dest.Extent,
			"sub-source", subSrc.Extent, "sub-dest", subDest.Extent,
		)
	}

	n := subDest.Copy(subSrc)
	if n != subDest.ByteSize() {
		d.log.Error("error copying data from partial extent", "expected", subDest.ByteSize(), "was", n)
	}

	return CachePosition{}, nil
}

func (d *Disk) readPartialExtent(
	ctx *Context,
	pe *PartialExtent,
	rngs []Extent,
	dataRange Extent,
	dest RangeData,
) error {
	src, _, err := d.er.fetchExtent(ctx, d.log, pe, nil)
	if err != nil {
		return err
	}

	isDebug := d.log.Enabled(ctx, slog.LevelDebug)

	// the bytes at the beginning of data are for LBA dataBegin.LBA.
	// the bytes at the beginning of rawData are for LBA full.LBA.
	// we want to compute the 2 byte ranges:
	//   1. the byte range for rng within data
	//   2. the byte range for rng within rawData
	// Then we copy the bytes from 2 to 1.
	for _, x := range rngs {
		overlap, ok := pe.Live.Clamp(x)
		if !ok {
			d.log.Error("error clamping required range to usable range", "request", x, "partial", pe.Live)
			return fmt.Errorf("error clamping range")
		}

		if isDebug {
			d.trace(ctx, "preparing to copy data from segment", "request", x, "clamped", overlap)
		}

		// Compute our source range and destination range against overlap

		subDest, ok := dest.SubRange(overlap)
		if !ok {
			d.log.Error("error clamping range", "full", pe.Live, "sub", overlap)
			return fmt.Errorf("error clamping range: %s => %s", pe.Live, overlap)
		}

		subSrc, ok := src.SubRange(overlap)
		if !ok {
			d.log.Error("error calculate source subrange",
				"input", src.Extent, "sub", overlap,
				"request", x, "usable", pe.Live,
				"full", pe.Extent,
			)
			return fmt.Errorf("error calculate source subrange")
		}

		if isDebug {
			d.trace(ctx, "copying segment data",
				"src", src.Extent,
				"dest", dest.Extent,
				"sub-source", subSrc.Extent, "sub-dest", subDest.Extent,
			)
		}

		n := subDest.Copy(subSrc)
		if n != subDest.ByteSize() {
			d.log.Error("error copying data from partial extent", "expected", subDest.ByteSize(), "was", n)
		}
	}

	return nil
}

func (d *Disk) ZeroBlocks(ctx context.Context, rng Extent) error {
	if d.readOnly {
		return nil
	}

	iops.Inc()
	blocksWritten.Add(float64(rng.Blocks))

	return d.curOC.ZeroBlocks(rng)
}

func (d *Disk) checkFlush(ctx context.Context) error {
	reason := d.curOC.ShouldFlush(FlushThreshHold)
	if reason == FlushNo {
		return nil
	}

	switch reason {
	case FlushTime:
		d.log.Info("flushing segment due to maximum segment lifetime",
			"age", time.Since(d.curOC.builder.openedAt),
			"threshold", MaxSegmentLifetime,
		)
	case FlushSize:
		d.log.Info("flushing segment due to size threshold",
			"body-size", d.curOC.BodySize(),
			"threshold", FlushThreshHold,
		)
	default:
		d.log.Warn("flushing segment for unknown reason", "reason", reason)
	}

	// Log segment statistics when flushing. This indicates a segment is closed and ready for upload.
	// Keys explained:
	//   body-size: compressed segment size in bytes
	//   extents: number of extents in the segment
	//   blocks: number of 4KB blocks covered by the extents
	//   input-bytes: total uncompressed data written (compression-rate = input-bytes / body-size)
	//   empty-blocks: number of blocks that were all zeros
	//   single-bes: number of extents with a single block
	//   compression-rate: average compression ratio (input / output)
	//   storage-ratio: inverse of compression rate (output / input)
	//   comp-rate-histo: histogram of per-extent compression ratios with buckets [1, 2, 3, 5, 10, 20, 50, 100, 200, 1000]
	//                    Each bucket counts extents with compression ratio less than that band threshold.
	//                    e.g., "[0 1910 3546 ...]" means 0 extents <1x, 1910 extents <2x, 3546 extents <3x, etc.
	d.log.Info("flushing segment",
		"body-size", d.curOC.BodySize(),
		"extents", d.curOC.Entries(),
		"blocks", d.curOC.TotalBlocks(),
		"input-bytes", d.curOC.InputBytes(),
		"empty-blocks", d.curOC.EmptyBlocks(),
		"single-bes", d.curOC.builder.singleBEs,
		"compression-rate", d.curOC.CompressionRate(),
		"storage-ratio", d.curOC.StorageRatio(),
		"comp-rate-histo", d.curOC.CompressionRateHistogram(),
	)

	ch, err := d.closeSegmentAsync(ctx)
	if err != nil {
		return err
	}

	if mode.Debug() {
		select {
		case <-ch:
			d.trace(ctx, "segment has been flushed")
		case <-ctx.Done():
		}
	}

	return nil
}

var ErrReadOnly = errors.New("disk open'd read-only")

func (d *Disk) WriteExtent(ctx context.Context, data RangeData) error {
	if d.readOnly {
		return ErrReadOnly
	}

	start := time.Now()

	defer func() {
		blocksWriteLatency.Observe(time.Since(start).Seconds())
	}()

	blocksWritten.Add(float64(data.Blocks))

	iops.Inc()

	err := d.curOC.WriteExtent(data)
	if err != nil {
		d.log.Error("error write extents to segment creator", "error", err)
		return err
	}

	return d.checkFlush(ctx)
}

func (d *Disk) Extents() int {
	return d.lba2pba.Len()
}

// WriteExtents writes multiple extents without performing any segment
// flush checking between them, thusly making sure that all of them end
// up in the same segment.
func (d *Disk) WriteExtents(ctx context.Context, ranges []RangeData) error {
	if d.readOnly {
		return ErrReadOnly
	}

	start := time.Now()

	defer func() {
		blocksWriteLatency.Observe(time.Since(start).Seconds())
	}()

	iops.Add(float64(len(ranges)))

	for _, data := range ranges {
		err := d.curOC.WriteExtent(data)
		if err != nil {
			d.log.Error("error write extents to segment creator", "error", err)
			return err
		}
	}

	return d.checkFlush(ctx)
}

func (d *Disk) SyncWriteCache() error {
	if d.readOnly {
		return nil
	}

	iops.Inc()

	if d.curOC != nil {
		return d.curOC.builder.Sync()
	}

	return nil
}

func (d *Disk) Close(ctx context.Context) error {
	if d.closed {
		return nil
	}

	err := d.finalizeSegment(ctx)
	if err != nil {
		return errors.Wrapf(err, "error closing segment")
	}

	done := make(chan EventResult)

	d.controller.EventsCh() <- Event{
		Kind: CleanupSegments,
		Done: done,
	}

	close(d.controller.EventsCh())

	<-done

	d.wg.Wait()

	err = d.saveLBAMap(ctx)
	if err != nil {
		d.log.Error("error saving LBA cached map", "error", err)
		err = errors.Wrapf(err, "error saving lba map")
	}

	d.er.Close()

	d.closed = true

	return err
}

func (d *Disk) Size() int64 {
	return d.size
}
