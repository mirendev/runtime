package lsvd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/lab47/lsvd/pkg/entropy"
	"github.com/pierrec/lz4/v4"
	"github.com/pkg/errors"
)

type FlushReason int

const (
	FlushNo FlushReason = iota
	FlushSize
	FlushTime
)

type SegmentCreator struct {
	log *slog.Logger

	builder *SegmentBuilder

	volName string

	buf []byte

	em *ExtentMap

	peScratch []PartialExtent
}

type SegmentBuilder struct {
	cnt int

	emptyBlocks   int
	totalBlocks   int
	singleBEs     int
	inputBytes    int64
	storageBytes  int64
	storageRatio  float64
	compRateHisto [10]int

	buf    []byte
	header bytes.Buffer

	offset  uint64
	extents []ExtentHeader

	comp    lz4.Compressor
	useZstd bool

	entropy entropy.Estimator

	path      string
	logF      *os.File
	logW      *bufio.Writer
	curOffset int64

	openedAt time.Time

	em *ExtentMap

	peScratch []PartialExtent
	affected  []ExtentLocation
}

const DefaultExtentsSize = 20000

var segBuilderPool = sync.Pool{
	New: func() any {
		return &SegmentBuilder{
			extents:  make([]ExtentHeader, 0, DefaultExtentsSize),
			affected: make([]ExtentLocation, 0, DefaultExtentsSize),
		}
	},
}

func NewSegmentBuilder() *SegmentBuilder {
	seg := segBuilderPool.Get().(*SegmentBuilder)
	seg.Reset()

	return seg
}

func ReturnSegmentBuilder(seg *SegmentBuilder) {
	segBuilderPool.Put(seg)
}

func (s *SegmentBuilder) Reset() {
	header := s.header
	header.Reset()

	*s = SegmentBuilder{
		peScratch: s.peScratch[:0],
		extents:   s.extents[:0],
		affected:  s.affected[:0],
		buf:       s.buf,
		header:    header,
	}
}

var histogramBands = []float64{1, 2, 3, 5, 10, 20, 50, 100, 200, 1000}

func NewSegmentCreator(log *slog.Logger, vol, path string) (*SegmentCreator, error) {
	oc := &SegmentCreator{
		log:     log,
		volName: vol,
		em:      NewExtentMap(),
		builder: NewSegmentBuilder(),
	}

	oc.builder.em = oc.em

	err := oc.builder.OpenWrite(path, log)
	if err != nil {
		return nil, err
	}

	return oc, nil
}

func (o *SegmentCreator) UseZstd() {
	o.builder.useZstd = true
}

func (o *SegmentBuilder) addToHistogram(val float64) {
	for i, v := range histogramBands {
		if v >= val {
			o.compRateHisto[i]++
			return
		}
	}
}

func (o *SegmentBuilder) Sync() error {
	if o.logW != nil {
		o.logW.Flush()
	}

	if o.logF != nil {
		return o.logF.Sync()
	}

	return nil
}

func (o *SegmentBuilder) OpenP() bool {
	return o.logF != nil
}

func (o *SegmentBuilder) OpenWrite(path string, log *slog.Logger) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	err = o.readLog(f, log)
	if err != nil {
		return errors.Wrapf(err, "error reading segment log")
	}

	o.logF = f
	o.logW = bufio.NewWriter(f)
	o.openedAt = time.Now()

	return nil
}

func (o *SegmentCreator) TotalBlocks() int {
	return o.builder.totalBlocks
}

func (o *SegmentCreator) ZeroBlocks(rng Extent) error {
	// The empty size will signal that it's empty blocks.
	aff, err := o.em.Update(o.log, ExtentLocation{
		ExtentHeader: ExtentHeader{
			Extent: rng,
		},
	}, o.peScratch[:0])
	if err != nil {
		return err
	}

	o.peScratch = aff[:0]

	return o.builder.ZeroBlocks(rng)
}

func (o *SegmentBuilder) ZeroBlocks(rng Extent) error {
	o.cnt++

	o.extents = append(o.extents, ExtentHeader{
		Extent: rng,
	})

	return nil
}

func (o *SegmentCreator) EmptyP() bool {
	return o.builder == nil || o.builder.cnt == 0
}

func (o *SegmentBuilder) ShouldFlush(sizeThreshold int) FlushReason {
	if o.BodySize() >= sizeThreshold {
		return FlushSize
	}

	if !o.openedAt.IsZero() && time.Since(o.openedAt) >= MaxSegmentLifetime {
		return FlushTime
	}

	return FlushNo
}

func (o *SegmentBuilder) BodySize() int {
	return int(o.offset)
}

func (o *SegmentCreator) ShouldFlush(sizeThreshold int) FlushReason {
	if o.EmptyP() {
		return FlushNo
	}

	return o.builder.ShouldFlush(sizeThreshold)
}

func (o *SegmentCreator) BodySize() int {
	return int(o.builder.offset)
}

func (o *SegmentCreator) Entries() int {
	return o.builder.cnt
}

func (o *SegmentCreator) EmptyBlocks() int {
	return o.builder.emptyBlocks
}

func (o *SegmentCreator) InputBytes() int64 {
	return o.builder.inputBytes
}

func (o *SegmentCreator) StorageBytes() int64 {
	return o.builder.storageBytes
}

func (o *SegmentCreator) CompressionRate() float64 {
	return float64(o.builder.inputBytes) / float64(o.builder.storageBytes)
}

func (o *SegmentCreator) StorageRatio() float64 {
	return float64(o.builder.storageBytes) / float64(o.builder.inputBytes)
}

func (o *SegmentCreator) AvgStorageRatio() float64 {
	return o.builder.storageRatio / float64(o.builder.cnt)
}

func (o *SegmentCreator) CompressionRateHistogram() []int {
	return o.builder.compRateHisto[:]
}

// writeLog writes the header and the data to the log so that we can
// recover the write with readLog if need be.
func (o *SegmentBuilder) writeLog(
	eh ExtentHeader,
	data []byte,
) (int, int, error) {
	dw := o.logW

	sz, err := eh.Write(dw)
	if err != nil {
		return 0, 0, err
	}

	n, err := dw.Write(data)
	if err != nil || n != len(data) {
		if err != nil {
			return 0, 0, err
		}

		return 0, 0, fmt.Errorf("short write to log: %d != %d", n, len(data))
	}

	return sz, sz + n, dw.Flush()
}

// readLog is used to restore the state of the SegmentCreator from the
// log written to data.
func (o *SegmentBuilder) readLog(f *os.File, log *slog.Logger) error {
	log.Debug("rebuilding memory from log", "path", f.Name())

	br := bufio.NewReader(f)

	for {
		var eh ExtentHeader

		hdrLen, err := eh.Read(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			log.Error("observed error reading extent header", "error", err)
			return err
		}

		trace(log, "read extent header", "extent", eh.Extent, "flags", eh.Flags(), "raw-size", eh.RawSize)

		o.totalBlocks += int(eh.Blocks)

		o.cnt++

		if eh.Size > 0 {
			n, err := br.Discard(int(eh.Size))
			if err != nil {
				return errors.Wrapf(err, "error copying body, expecting %d, got %d", eh.Size, n)
			}
			if n != int(eh.Size) {
				return fmt.Errorf("short copy: %d != %d", n, eh.Size)
			}

			if eh.RawSize > 0 {
				o.storageRatio += float64(eh.Size) / float64(eh.RawSize)
			} else {
				o.storageRatio += 1
			}

			// Update offset to match where it is in body
			eh.Offset = uint32(o.offset) + uint32(hdrLen)
			trace(log, "log rebuild offset", "extent", eh.Extent, "offset", eh.Offset)
		}

		o.extents = append(o.extents, eh)

		aff, err := o.em.Update(log, ExtentLocation{
			ExtentHeader: eh,
		}, o.peScratch[:0])
		if err != nil {
			return err
		}

		o.peScratch = aff

		o.offset += (uint64(eh.Size) + uint64(hdrLen))
	}

	return nil
}

// FillExtent attempts to fill as much of +data+ as possible, returning
// a list of Extents that was unable to fill. That later list is then
// feed to the system that reads data from segments.
func (o *SegmentCreator) FillExtent(ctx *Context, data RangeDataView) ([]Extent, error) {
	startFill := time.Now()

	rng := data.Extent

	ranges, err := o.em.Resolve(o.log, rng, o.peScratch[:0])
	if err != nil {
		return nil, err
	}

	var ret []Extent

	var compTime time.Duration
	marker := ctx.Marker()

	for _, srcRng := range ranges {
		ctx.ResetTo(marker)

		subDest, ok := data.SubRange(srcRng.Live)
		if !ok {
			o.log.Error("error calculating subrange")
			return nil, fmt.Errorf("error calculating subrange")
		}

		trace(o.log, "calculating relevant ranges",
			"data", rng,
			"src", srcRng.Live,
			"dest", subDest.Extent,
			"offset", srcRng.Offset,
		)

		ret = append(ret, subDest.Extent)

		// It's a empty range
		if srcRng.Size == 0 {
			continue
		}

		trace(o.log, "reading partial from write cache", "rng", srcRng.Live, "dest", subDest.Extent, "flags", srcRng.Flags())

		var srcData []byte

		switch srcRng.Flags() {
		case Uncompressed:
			if len(o.buf) < int(srcRng.Size) {
				o.buf = make([]byte, srcRng.Size)
			}

			srcData = o.buf[:srcRng.Size]

			offset := srcRng.Offset // + (uint32(subDest.LBA-srcRng.LBA) * BlockSize)
			trace(o.log, "reading uncompressed from write log", "src", srcRng.Live, "dest", subDest.Extent, "byte-offset", offset)
			n, err := o.builder.logF.ReadAt(srcData, int64(offset))
			if err != nil {
				return nil, err
			}

			if n != len(srcData) {
				return nil, fmt.Errorf("reading from write log returned wrong number of bytes (%d, %d)", n, subDest.ByteSize())
			}
		case Compressed:
			s := time.Now()
			origSize := srcRng.Size // Size is the "on-disk" size, ie the compressed size

			if len(o.buf) < int(origSize) {
				o.buf = make([]byte, origSize)
			}

			srcData = o.buf[:origSize]

			n, err := o.builder.logF.ReadAt(srcData, int64(srcRng.Offset))
			if err != nil {
				trace(o.log, "file size on read failure", "size", o.builder.offset)
				return nil, errors.Wrapf(err, "reading extent at %d:%d (%d, %d)",
					srcRng.Offset, srcRng.Size, len(srcData), srcRng.RawSize)
			}

			if n != len(srcData) {
				return nil, fmt.Errorf("reading from write log returned wrong number of bytes 2 (%d, %d)", n, len(srcData))
			}

			trace(o.log, "compressed range", "offset", srcRng.Offset)

			trace(o.log, "original size of compressed extent", "len", srcRng.RawSize, "comp-size", srcRng.Size)

			uncompData := ctx.Allocate(int(srcRng.RawSize))

			n, err = lz4.UncompressBlock(srcData, uncompData)
			if err != nil {
				return nil, fmt.Errorf("fill-extent: error uncompressing (src=%d, dest=%d): %w", len(srcData), len(uncompData), err)
			}

			if n > int(srcRng.RawSize) {
				o.log.Warn("unusual long write detected", "expected", origSize, "actual", n, "buf-len", len(o.buf))
			} else if n < int(srcRng.RawSize) {
				return nil, fmt.Errorf("didn't fill destination (%d != %d)", n, origSize)
			}

			srcData = uncompData

			compTime += time.Since(s)
		case Empty:
			// handled above, shouldn't be here.
			return nil, fmt.Errorf("invalid flag 2, should have size == 0, did not")
		default:
			return nil, fmt.Errorf("invalid flag %d", srcRng.Flags())
		}

		src := MapRangeData(srcRng.Extent, srcData)

		subSrc, ok := src.SubRange(subDest.Extent)
		if !ok {
			o.log.Error("error calculating src subrange")
			return nil, fmt.Errorf("error calculating src subrange")
		}

		trace(o.log, "mapping src range", "rng", subSrc.Extent)

		n := subDest.Copy(subSrc)

		trace(o.log, "copied range", "bytes", n, "blocks", n/BlockSize)
	}

	e := time.Since(startFill)

	readProcessing.Add(e.Seconds())
	compressionOverhead.Add(compTime.Seconds())

	o.peScratch = ranges[:0]

	return ret, nil
}

func (o *SegmentCreator) WriteExtent(ext RangeData) error {
	_, eh, err := o.builder.WriteExtent(o.log, ext.View())
	if err != nil {
		return err
	}

	if o.em == nil {
		o.em = NewExtentMap()
	}

	aff, err := o.em.Update(o.log, ExtentLocation{
		ExtentHeader: eh,
	}, o.peScratch[:0])

	if err != nil {
		return err
	}

	o.peScratch = aff[:0]

	return nil
}

type SegmentStats struct {
	Blocks     uint64
	TotalBytes uint64
	DataOffset uint32
}

func (o *SegmentCreator) Flush(ctx context.Context,
	vol Volume, seg SegmentId,
) ([]ExtentLocation, *SegmentStats, error) {
	if o.builder == nil {
		return nil, nil, fmt.Errorf("segment builder is closed")
	}
	locs, stats, err := o.builder.Flush(ctx, o.log, vol, seg, o.volName)
	if err != nil {
		return locs, stats, err
	}

	return locs, stats, nil
}

func (o *SegmentCreator) Close() error {
	err := o.builder.Close(o.log)

	ReturnSegmentBuilder(o.builder)
	o.builder = nil

	return err
}

func (o *SegmentBuilder) Close(log *slog.Logger) error {
	if o.logF != nil {
		o.logF.Close()

		log.Debug("removing log file", "name", o.logF.Name())
		err := os.Remove(o.logF.Name())
		if err != nil {
			log.Error("error removing log file", "error", err)
		}
	}

	return nil
}

const entropyLimit = 7.0

func (o *SegmentBuilder) WriteExtent(log *slog.Logger, ext RangeDataView) ([]byte, ExtentHeader, error) {
	extBytes := ext.ByteSize()
	if o.buf == nil {
		o.buf = make([]byte, extBytes*2)
	}

	o.totalBlocks += int(ext.Blocks)

	var data []byte

	eh := ExtentHeader{
		Extent: ext.Extent,
	}

	o.cnt++

	if ext.EmptyP() {
		o.emptyBlocks += int(ext.Blocks)
	} else {
		if ext.Blocks == 1 {
			o.singleBEs++
		}

		input := ext.ReadData()
		o.inputBytes += int64(len(input))

		if o.entropy == nil {
			o.entropy = entropy.NewEstimator()
		}

		o.entropy.Reset()
		o.entropy.Write(ext.ReadData())

		var (
			useCompression bool
			compressedSize int
			err            error
		)

		if o.entropy.Value() <= entropyLimit {
			bound := lz4.CompressBlockBound(extBytes)

			if len(o.buf) < bound {
				o.buf = make([]byte, bound)
			}

			compressedSize, err = o.comp.CompressBlock(ext.ReadData(), o.buf)
			if err != nil {
				return nil, eh, err
			}

			// Only keep compression greater than 1.5x
			if compressedSize > 0 && ((compressedSize*3)/2) < extBytes {
				useCompression = true
			}
		}

		if useCompression {
			eh.RawSize = uint32(extBytes)
			eh.Size = uint32(compressedSize)

			data = o.buf[:compressedSize]

			o.addToHistogram(float64(len(input)) / float64(len(data)))
		} else {
			eh.Size = uint32(extBytes)

			data = ext.ReadData()

			o.addToHistogram(1)
		}

		o.storageBytes += int64(len(data))
		o.storageRatio += (float64(len(data)) / float64(len(input)))
	}

	hdr, n, err := o.writeLog(eh, data)
	if err != nil {
		return nil, eh, err
	}

	eh.Offset = uint32(o.offset) + uint32(hdr)

	o.offset += uint64(n)

	if log.Enabled(context.TODO(), LevelTrace) {
		trace(log, "wrote range",
			"offset", eh.Offset,
			"size", eh.Size,
			"raw-size", eh.RawSize,
			"blocks", eh.Blocks,
			"offset", eh.Offset,
		)
	}
	o.extents = append(o.extents, eh)

	return data, eh, nil
}

func (o *SegmentBuilder) Flush(ctx context.Context, log *slog.Logger,
	vol Volume, seg SegmentId, volName string,
) ([]ExtentLocation, *SegmentStats, error) {
	start := time.Now()
	defer func() {
		segmentTime.Observe(time.Since(start).Seconds())
	}()

	stats := &SegmentStats{}

	for _, blk := range o.extents {
		stats.Blocks += uint64(blk.Blocks)

		if log.Enabled(context.TODO(), LevelTrace) {
			trace(log, "writing extent to header", "extent", blk.Extent, "offset", blk.Offset, "blocks", blk.Blocks)
		}

		_, err := blk.Write(&o.header)
		if err != nil {
			return nil, nil, err
		}
	}

	dataBegin := uint32(o.header.Len() + 8)

	if log.Enabled(context.TODO(), LevelTrace) {
		trace(log, "segment constructed",
			"header-size", o.header.Len(),
			"body-size", o.offset,
			"blocks", len(o.extents),
		)
	}

	entries := o.affected[:0]

	stats.DataOffset = dataBegin

	writtenBytes.Add(float64(o.inputBytes))
	segmentsBytes.Add(float64(o.storageBytes))

	for _, eh := range o.extents {
		eh.Offset += dataBegin
		entries = append(entries, ExtentLocation{
			ExtentHeader: eh,
			Segment:      seg,
		})

		if log.Enabled(context.TODO(), LevelTrace) {
			trace(log, "advertising extent", "extent", eh.Extent, "offset", eh.Offset, "blocks", eh.Blocks)
		}
	}

	o.affected = entries[:0]

	completedPath := o.path + ".complete"

	defer os.Remove(completedPath)

	f, err := os.Create(completedPath)
	if err != nil {
		return nil, nil, err
	}

	defer f.Close()

	err = SegmentHeader{
		ExtentCount: uint32(o.cnt),
		DataOffset:  dataBegin,
	}.Write(f)
	if err != nil {
		return nil, nil, err
	}

	n, err := io.Copy(f, bytes.NewReader(o.header.Bytes()))
	if err != nil {
		return nil, nil, err
	}

	stats.TotalBytes += uint64(n)

	_, err = o.logF.Seek(0, io.SeekStart)
	if err != nil {
		return nil, nil, err
	}

	n, err = io.Copy(f, o.logF)
	if err != nil {
		return nil, nil, err
	}

	stats.TotalBytes += uint64(n)

	f.Seek(0, io.SeekStart)

	var diskExtents []ExternalExtentHeader

	for _, blk := range o.extents {
		var de ExternalExtentHeader
		de.SetLba(uint64(blk.LBA))
		de.SetBlocks(blk.Blocks)
		de.SetOffset(blk.Offset + dataBegin)
		de.SetSize(blk.Size)
		de.SetRawSize(blk.RawSize)

		diskExtents = append(diskExtents, de)
	}

	layout := &SegmentLayout{}
	layout.SetExtents(diskExtents)

	err = vol.NewSegment(ctx, seg, layout, f)
	if err != nil {
		return nil, nil, err
	}

	/*
		err = sa.UploadSegment(ctx, seg, f)
		if err != nil {
			return nil, nil, err
		}

		err = sa.AppendToSegments(ctx, volName, seg)
		if err != nil {
			return nil, nil, err
		}
	*/

	log.Info("segment persistent to storage", "segment", seg, "volume", volName,
		"blocks", stats.Blocks,
		"size", stats.TotalBytes)

	return entries, stats, nil
}
