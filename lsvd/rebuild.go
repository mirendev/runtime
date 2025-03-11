package lsvd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/oklog/ulid/v2"
	"github.com/pkg/errors"
)

func (d *Disk) rebuildFromSegments(ctx context.Context) error {
	for idx, ld := range d.readDisks {
		// We don't populate from... ourselves.
		if ld == d {
			continue
		}

		ld.lba2pba.Populate(d.log, d.lba2pba, uint16(idx))
	}

	entries, err := d.volume.ListSegments(ctx)
	if err != nil {
		return err
	}

	for _, ent := range entries {
		err := d.rebuildFromSegment(ctx, ent)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Disk) rebuildFromSegment(ctx context.Context, seg SegmentId) error {
	d.log.Info("rebuilding mappings from segment", "id", seg)

	f, err := d.volume.OpenSegment(ctx, seg)
	if err != nil {
		return err
	}

	defer f.Close()

	layout, err := f.Layout(ctx)
	if err != nil {
		return err
	}

	stats := &SegmentStats{}

	if layout != nil {
		d.log.Debug("loading extents from segment layout", "extents", len(layout.Extents()))

		for _, ext := range layout.Extents() {
			stats.Blocks += uint64(ext.Blocks())

			var eh ExtentHeader
			eh.LBA = LBA(ext.Lba())
			eh.Blocks = uint32(ext.Blocks())
			eh.Offset = ext.Offset()
			eh.Size = ext.Size()
			eh.RawSize = ext.RawSize()

			affected, err := d.lba2pba.Update(d.log, ExtentLocation{
				ExtentHeader: eh,
				Segment:      seg,
			}, nil)
			if err != nil {
				return err
			}

			d.s.UpdateUsage(d.log, seg, affected)
		}
	} else {
		br := bufio.NewReader(ToReader(f))

		var hdr SegmentHeader

		err = hdr.Read(br)
		if err != nil {
			return err
		}

		d.log.Debug("extent header info", "count", hdr.ExtentCount, "data-begin", hdr.DataOffset)

		for i := uint32(0); i < hdr.ExtentCount; i++ {
			var eh ExtentHeader

			_, err := eh.Read(br)
			if err != nil {
				return err
			}

			stats.Blocks += uint64(eh.Blocks)

			eh.Offset += hdr.DataOffset

			affected, err := d.lba2pba.Update(d.log, ExtentLocation{
				ExtentHeader: eh,
				Segment:      seg,
			}, nil)
			if err != nil {
				return err
			}

			d.s.UpdateUsage(d.log, seg, affected)
		}
	}

	// Now reset the stats for our seg to the correct ones.
	d.s.Create(seg, stats)

	return nil
}

func (d *Disk) restoreWriteCache(ctx context.Context) error {
	entries, err := filepath.Glob(filepath.Join(d.path, "writecache.*"))
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	d.log.Info("restoring write cache", "entries", entries)

	for _, ent := range entries {
		err := d.restoreWriteCacheFile(ctx, ent)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Disk) restoreWriteCacheFile(ctx context.Context, path string) error {
	oc, err := NewSegmentCreator(d.log, d.volName, path)
	if err != nil {
		return err
	}

	d.curSeq, err = d.nextSeq()
	if err != nil {
		return err
	}

	d.curOC = oc

	return nil
}

func (d *Disk) saveLBAMap(ctx context.Context) error {
	f, err := os.Create(filepath.Join(d.path, "head.map"))
	if err != nil {
		return err
	}

	defer f.Close()

	sh, err := d.segmentsHash(ctx)
	if err != nil {
		return errors.Wrapf(err, "calculating segments hash")
	}

	hdr := &lbaCacheMapHeader{
		CreatedAt:    time.Now(),
		SegmentsHash: sh,
		Stats:        make(map[string]segmentStats),
	}

	for seg, stats := range d.s.segments {
		if stats.deleted {
			continue
		}

		hdr.Stats[seg.String()] = segmentStats{
			Size: stats.Size,
			Used: stats.Used,
		}
	}

	return saveLBAMap(d.lba2pba, f, hdr)
}

func (d *Disk) segmentsHash(ctx context.Context) (string, error) {
	segments, err := d.volume.ListSegments(ctx)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	for _, s := range segments {
		h.Write(s[:])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func (d *Disk) loadLBAMap(ctx context.Context) (bool, error) {
	f, err := os.Open(filepath.Join(d.path, "head.map"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, err
	}

	defer f.Close()

	d.log.Debug("reloading lba map from head.map")

	sh, err := d.segmentsHash(ctx)
	if err != nil {
		return false, errors.Wrapf(err, "calculating segments hash")
	}

	m, hdr, err := processLBAMap(d.log, f)
	if err != nil {
		return false, err
	}

	if hdr.SegmentsHash != sh {
		d.log.Warn("ignoring out of date head.map",
			"created-at", hdr.CreatedAt,
			"expected", sh,
			"actual", hdr.SegmentsHash,
		)

		return false, nil
	}

	d.log.Info("validated cached lba map", "created-at", hdr.CreatedAt, "hash", sh)

	d.lba2pba = m

	var total, used uint64

	for seg, stats := range hdr.Stats {
		id, err := ulid.Parse(seg)
		if err != nil {
			d.log.Error("invalid segment id in segment stats", "segment", seg)
			continue
		}

		total += stats.Size
		used += stats.Used

		seg := SegmentId(id)

		d.s.Create(seg, &SegmentStats{
			Blocks: stats.Size,
		})

		trace(d.log, "initialized segment", "segment", seg, "size", stats.Size, "used", stats.Used)
		d.s.SetSegment(seg, stats.Size, stats.Used)
	}

	d.log.Info("initialized segments from LBA cache",
		"segments", len(hdr.Stats),
		"total", total,
		"used", used,
		"density", 100*(float64(used)/float64(total)),
	)

	d.lba2pba = m

	return true, nil
}

type segmentStats struct {
	Size uint64 `json:"used" cbor:"1,keyasint"`
	Used uint64 `json:"size" cbor:"2,keyasint"`
}

type lbaCacheMapHeader struct {
	CreatedAt    time.Time               `json:"created_at" cbor:"created_at"`
	SegmentsHash string                  `json:"segments_hash" cbor:"segments_hash"`
	Stats        map[string]segmentStats `json:"segment_stats" cbor:"segment_stats"`
}

func saveLBAMap(m *ExtentMap, f io.Writer, hdr *lbaCacheMapHeader) error {
	bw := bufio.NewWriter(f)
	defer bw.Flush()

	enc := cbor.NewEncoder(f)
	err := enc.Encode(hdr)
	if err != nil {
		return err
	}

	for it := m.LockedIterator(); it.Valid(); it.Next() {
		cur := it.Value()

		err := enc.Encode(cur)
		if err != nil {
			return err
		}
	}

	return nil
}

func processLBAMap(log *slog.Logger, f io.Reader) (*ExtentMap, *lbaCacheMapHeader, error) {
	m := NewExtentMap()

	br := bufio.NewReader(f)
	dec := cbor.NewDecoder(br)

	var hdr lbaCacheMapHeader

	err := dec.Decode(&hdr)
	if err != nil {
		return nil, nil, err
	}

	for {
		var (
			pba PartialExtent
		)

		err := dec.Decode(&pba)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, nil, err
		}

		// log.Trace("read from lba map", "extent", pba.Live, "flag", pba.Flags)

		m.set(pba)
	}

	return m, &hdr, nil
}
