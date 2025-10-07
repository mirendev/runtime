package lsvd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lab47/lz4decode"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func isEmpty(d []byte) bool {
	for _, b := range d {
		if b != 0 {
			return false
		}
	}

	return true
}

var (
	testData  = make([]byte, 4*1024)
	testData2 = make([]byte, 4*1024)
	testData3 = make([]byte, 4*1024)

	testExtent  RawBlocks
	testExtent2 RawBlocks
	testExtent3 RawBlocks

	testRand  = make([]byte, 4*1024)
	testRandX RawBlocks

	testEmpty  = make([]byte, BlockSize)
	testEmptyX RawBlocks
)

func init() {
	for i := 0; i < 10; i++ {
		testData[i] = 0x47
	}

	for i := 0; i < 10; i++ {
		testData2[i] = 0x48
	}

	for i := 0; i < 10; i++ {
		testData3[i] = 0x49
	}

	testExtent = BlockDataView(testData)
	testExtent2 = BlockDataView(testData2)
	testExtent3 = BlockDataView(testData3)

	io.ReadFull(rand.Reader, testRand)
	testRandX = BlockDataView(testRand)

	testEmptyX = BlockDataView(testEmpty)
}

func blockEqual(t *testing.T, a, b []byte) {
	t.Helper()
	if !bytes.Equal(a, b) {
		t.Error("blocks are not the same")
	}
	//require.True(t, bytes.Equal(a, b), "blocks are not the same")
}

func extentEqual(t *testing.T, actual RawBlocks, expected RangeData) {
	t.Helper()

	require.Equal(t, actual.Blocks(), expected.Blocks)

	if !bytes.Equal(actual, expected.ReadData()) {
		t.Error("blocks are not the same")
	}
}

func TestLSVD(t *testing.T) {
	log := slog.Default()

	gctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx := NewContext(gctx)

	t.Run("reads with no data return zeros", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		data, err := d.ReadExtent(ctx, Extent{LBA: 1, Blocks: 1})
		r.NoError(err)

		//r.Nil(data.ReadData(), "data shouldn't be allocated")

		r.True(isEmpty(data.ReadData()))
	})

	t.Run("writes are returned by next read", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		aff, err := d.curOC.em.Resolve(log, Extent{0, 1}, nil)
		r.NoError(err)

		r.Len(aff, 1)

		r.Equal(uint32(4096), aff[0].Size)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d2)
	})

	t.Run("can read from across writes from the write cache", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		data := NewRangeData(ctx, Extent{0, 10})
		_, err = io.ReadFull(rand.Reader, data.WriteData())
		r.NoError(err)

		err = d.WriteExtent(ctx, data)
		r.NoError(err)

		err = d.WriteExtent(ctx, testRandX.MapTo(1))
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 4})
		r.NoError(err)

		n := d2.ReadData()
		blockEqual(t, data.ReadData()[:BlockSize], n[:BlockSize])
		n = n[BlockSize:]
		blockEqual(t, testRandX, n[:BlockSize])
		n = n[BlockSize:]
		blockEqual(t, data.ReadData()[BlockSize*2:BlockSize*4], n)
	})

	t.Run("can read the middle of an write cache range", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		data := NewRangeData(ctx, Extent{1, 19})
		_, err = io.ReadFull(rand.Reader, data.WriteData())
		r.NoError(err)

		err = d.WriteExtent(ctx, data)
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 4, Blocks: 8})
		r.NoError(err)

		blockEqual(t, data.ReadData()[BlockSize*3:BlockSize*11], d2.ReadData())
	})

	t.Run("can read from segments", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		t.Logf("data sum: %s", rangeSum(testRandX))

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		t.Log("closing")
		r.NoError(d.Close(ctx))

		t.Log("reopening disk")
		d, err = NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d2)

		t.Run("and from the read cache", func(t *testing.T) {
			r := require.New(t)

			d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testRandX, d2)
		})
	})

	t.Run("can read from partial segments", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		const blocks = 4

		big := make([]byte, blocks*4*1024)

		_, err = io.ReadFull(rand.Reader, big)
		r.NoError(err)

		err = d.WriteExtent(ctx, MapRangeData(Extent{0, blocks}, big))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		d, err = NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 1, Blocks: 1})
		r.NoError(err)

		blockEqual(t, d2.ReadData(), big[BlockSize:BlockSize+BlockSize])

		t.Run("and from the read cache", func(t *testing.T) {
			r := require.New(t)

			d2, err := d.ReadExtent(ctx, Extent{LBA: 1, Blocks: 1})
			r.NoError(err)

			blockEqual(t, d2.ReadData(), big[BlockSize:BlockSize+BlockSize])
		})
	})

	t.Run("writes to clear blocks don't corrupt the cache", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		r.NoError(d.WriteExtent(ctx, testEmptyX.MapTo(47)))

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d2)
	})

	t.Run("stale reads aren't returned", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(0))
		r.NoError(err)

		t.Log("closing disk")
		r.NoError(d.Close(ctx))

		t.Log("reopening disk")
		d, err = NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testExtent, d2)

		err = d.WriteExtent(ctx, testExtent2.MapTo(0))
		r.NoError(err)

		d3, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testExtent2, d3)
	})

	t.Run("writes written out to an segment", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		var ur UlidRecall

		d, err := NewDisk(ctx, log, tmpdir, WithSeqGen(ur.Gen))
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		t.Log("closing disk")
		r.NoError(d.Close(ctx))

		t.Log("reopening disk")
		f, err := os.Open(filepath.Join(tmpdir, "segments", "segment."+ur.First().String()))
		r.NoError(err)

		defer f.Close()

		br := bufio.NewReader(f)

		var cnt uint32
		err = binary.Read(br, binary.BigEndian, &cnt)
		r.NoError(err)

		r.Equal(uint32(1), cnt)

		var hdrLen uint32
		err = binary.Read(br, binary.BigEndian, &hdrLen)
		r.NoError(err)

		r.Equal(uint32(0xe), hdrLen)

		lba, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(47), lba)

		blocks, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(1), blocks)

		blkSize, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(0x28), blkSize)

		offset, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(6), offset)

		rawSize, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(BlockSize), rawSize)

		_, err = f.Seek(int64(uint64(hdrLen)+offset), io.SeekStart)
		r.NoError(err)

		view := make([]byte, BlockSize)

		buf := make([]byte, blkSize)

		_, err = io.ReadFull(f, buf)
		r.NoError(err)

		sz, err := lz4decode.UncompressBlock(buf, view, nil)
		r.NoError(err)

		view = view[:sz]

		blockEqual(t, testData, view)

		g, err := os.Open(filepath.Join(tmpdir, "volumes", "default", "segments"))
		r.NoError(err)

		defer g.Close()

		gbr := bufio.NewReader(g)

		var iseg SegmentId

		_, err = gbr.Read(iseg[:])
		r.NoError(err)

		r.Equal(ulid.ULID(iseg), ur.First())
	})

	t.Run("segments that can't be compressed are flagged", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		var ur UlidRecall

		d, err := NewDisk(ctx, log, tmpdir, WithSeqGen(ur.Gen))
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testRandX.MapTo(47))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		f, err := os.Open(filepath.Join(tmpdir, "segments", "segment."+ur.First().String()))
		r.NoError(err)

		defer f.Close()

		br := bufio.NewReader(f)

		var cnt uint32
		err = binary.Read(br, binary.BigEndian, &cnt)
		r.NoError(err)

		r.Equal(uint32(1), cnt)

		var hdrLen uint32
		err = binary.Read(br, binary.BigEndian, &hdrLen)
		r.NoError(err)

		r.Equal(uint32(4+10), hdrLen)

		lba, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(47), lba)

		bloccks, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(1), bloccks)

		blkSize, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(BlockSize), blkSize)

		offset, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(6), offset)

		_, err = f.Seek(int64(uint64(hdrLen)+offset), io.SeekStart)
		r.NoError(err)

		view := make([]byte, blkSize)

		_, err = io.ReadFull(f, view)
		r.NoError(err)

		blockEqual(t, testRand, view)

		d2, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		x1, err := d2.ReadExtent(ctx, Extent{LBA: 47, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, x1)
	})

	t.Run("empty blocks are flagged specially", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		var ur UlidRecall

		d, err := NewDisk(ctx, log, tmpdir, WithSeqGen(ur.Gen))
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testEmptyX.MapTo(47))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		f, err := os.Open(filepath.Join(tmpdir, "segments", "segment."+ur.First().String()))
		r.NoError(err)

		defer f.Close()

		br := bufio.NewReader(f)

		var cnt uint32
		err = binary.Read(br, binary.BigEndian, &cnt)
		r.NoError(err)

		r.Equal(uint32(1), cnt)

		var hdrLen uint32
		err = binary.Read(br, binary.BigEndian, &hdrLen)
		r.NoError(err)

		r.Equal(uint32(3+10), hdrLen)

		lba, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(47), lba)

		blocks, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(1), blocks)

		blkSize, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(0), blkSize)

		offset, err := binary.ReadUvarint(br)
		r.NoError(err)

		r.Equal(uint64(5), offset)
	})

	t.Run("reads empty from a previous empty write", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testEmptyX.MapTo(0))
		r.NoError(err)

		data, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		r.True(isEmpty(data.RawBlocks().BlockView(0)))

		data, err = d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		r.True(isEmpty(data.RawBlocks().BlockView(0)))

		r.NoError(d.Close(ctx))

		d, err = NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		copy(data.RawBlocks().BlockView(0), testRand)

		data, err = d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		r.True(isEmpty(data.RawBlocks().BlockView(0)))
	})

	t.Run("can access blocks from the log", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 47, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testExtent, d2)
	})

	t.Run("can access blocks from the log when the check isn't active", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		err = d.CloseSegment(ctx)
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 47, Blocks: 1})
		r.NoError(err)

		blockEqual(t, d2.RawBlocks().BlockView(0), testExtent[:BlockSize])
	})

	t.Run("rebuilds the LBA mappings", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		d.lba2pba.m.Clear()

		err = d.CloseSegment(ctx)
		r.NoError(err)

		d.lba2pba.m.Clear()

		r.NoError(d.rebuildFromSegments(ctx))
		r.NotZero(d.lba2pba.Len())

		_, ok := d.lba2pba.m.Get(47)
		r.True(ok)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 47, Blocks: 1})
		r.NoError(err)

		blockEqual(t, d2.RawBlocks().BlockView(0), testData)
	})

	t.Run("tracks segment size and used blocks correctly with non-overlapping extents", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		// Write 10 non-overlapping extents of various lengths
		// LBA 0-9: 10 blocks
		// LBA 100-104: 5 blocks
		// LBA 200-202: 3 blocks
		// LBA 300-307: 8 blocks
		// LBA 400-400: 1 block
		// LBA 500-511: 12 blocks
		// LBA 600-605: 6 blocks
		// LBA 700-701: 2 blocks
		// LBA 800-814: 15 blocks
		// LBA 900-903: 4 blocks
		// Total: 66 blocks

		extents := []struct {
			lba    LBA
			blocks int
		}{
			{0, 10},
			{100, 5},
			{200, 3},
			{300, 8},
			{400, 1},
			{500, 12},
			{600, 6},
			{700, 2},
			{800, 15},
			{900, 4},
		}

		totalBlocks := 0
		for _, ext := range extents {
			// Create test data for this extent
			data := make([]byte, ext.blocks*BlockSize)
			for i := range data {
				data[i] = byte(ext.lba)
			}
			rb := BlockDataView(data)

			err = d.WriteExtent(ctx, rb.MapTo(ext.lba))
			r.NoError(err)

			totalBlocks += ext.blocks
		}

		// Close the segment to flush it
		err = d.CloseSegment(ctx)
		r.NoError(err)

		// Get the segment ID that was written
		segments, err := d.volume.ListSegments(ctx)
		r.NoError(err)
		r.Len(segments, 1, "should have exactly one segment")

		segId := segments[0]

		// Check segment stats before rebuild
		segTotal, segUsed, segExtents := d.s.SegmentInfo(segId)
		t.Logf("Before rebuild - Segment %s: total=%d, used=%d, extents=%d", segId, segTotal, segUsed, segExtents)
		r.Equal(uint64(totalBlocks), segTotal, "segment total should equal total blocks written")
		r.Equal(uint64(totalBlocks), segUsed, "segment used should equal total blocks (no overlaps)")
		r.Equal(len(extents), segExtents, "segment should have correct extent count")

		// Now create a new disk - it will automatically rebuild from segments
		d2, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d2.Close(ctx)

		// Check segment stats after rebuild (NewDisk already called rebuildFromSegments)
		segTotal2, segUsed2, segExtents2 := d2.s.SegmentInfo(segId)
		t.Logf("After rebuild - Segment %s: total=%d, used=%d, extents=%d", segId, segTotal2, segUsed2, segExtents2)

		// These should match the original values
		r.Equal(segTotal, segTotal2, "segment total should be preserved after rebuild")
		r.Equal(segUsed, segUsed2, "segment used should be preserved after rebuild")
		r.Equal(segExtents, segExtents2, "segment extent count should be preserved after rebuild")

		// Verify all extents are readable
		for _, ext := range extents {
			readData, err := d2.ReadExtent(ctx, Extent{LBA: ext.lba, Blocks: uint32(ext.blocks)})
			r.NoError(err)
			r.NotNil(readData)

			// Verify data content
			rb := readData.RawBlocks()
			for i := 0; i < ext.blocks; i++ {
				block := rb.BlockView(i)
				r.Equal(byte(ext.lba), block[0], "block should contain correct marker byte")
			}
		}

		// Verify UsedBlocks returns the correct total
		totalUsed := d2.s.UsedBlocks()
		t.Logf("Total used blocks across all segments: %d", totalUsed)
		r.Equal(uint64(totalBlocks), totalUsed, "UsedBlocks should return correct total")
	})

	t.Run("tracks segment size and used blocks correctly with overlapping extents", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		// Write extents with overlaps
		// First write some extents
		extents := []struct {
			lba    LBA
			blocks int
		}{
			{0, 10},   // Blocks 0-9
			{100, 20}, // Blocks 100-119
			{200, 15}, // Blocks 200-214
		}

		var totalBlocksWritten int
		for _, ext := range extents {
			data := make([]byte, ext.blocks*int(BlockSize))
			// Mark each block with its LBA for verification
			for i := 0; i < ext.blocks; i++ {
				data[i*int(BlockSize)] = byte(ext.lba)
			}
			err := d.WriteExtent(ctx, BlockDataView(data).MapTo(ext.lba))
			r.NoError(err)
			totalBlocksWritten += ext.blocks
		}

		// Now overwrite some of the extents
		overlaps := []struct {
			lba    LBA
			blocks int
		}{
			{5, 3},    // Overlaps with first extent (blocks 5-7)
			{110, 5},  // Overlaps with second extent (blocks 110-114)
			{200, 10}, // Overlaps with third extent (blocks 200-209)
		}

		for _, ext := range overlaps {
			data := make([]byte, ext.blocks*int(BlockSize))
			for i := 0; i < ext.blocks; i++ {
				data[i*int(BlockSize)] = byte(ext.lba + 128) // Different marker
			}
			err := d.WriteExtent(ctx, BlockDataView(data).MapTo(ext.lba))
			r.NoError(err)
			totalBlocksWritten += ext.blocks
		}

		// Total blocks written = 10 + 20 + 15 + 3 + 5 + 10 = 63
		// Total unique blocks = 10 + 20 + 15 = 45 (the overlaps don't add new unique blocks)
		expectedTotalBlocks := 63
		expectedUsedBlocks := 45

		err = d.CloseSegment(ctx)
		r.NoError(err)

		segments := d.s.LiveSegments()
		r.Len(segments, 1, "should have exactly one segment")

		segId := segments[0]

		// Check segment stats before rebuild
		segTotal, segUsed, segExtents := d.s.SegmentInfo(segId)
		t.Logf("Before rebuild - Segment %s: total=%d, used=%d, extents=%d", segId, segTotal, segUsed, segExtents)
		r.Equal(uint64(expectedTotalBlocks), segTotal, "segment total should equal total blocks written (including overlaps)")
		r.Equal(uint64(expectedUsedBlocks), segUsed, "segment used should reflect only live blocks after overlaps")
		r.Equal(len(extents)+len(overlaps), segExtents, "segment should have correct extent count")

		// Now create a new disk - it will automatically rebuild from segments
		d2, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d2.Close(ctx)

		// Check segment stats after rebuild
		segTotal2, segUsed2, segExtents2 := d2.s.SegmentInfo(segId)
		t.Logf("After rebuild - Segment %s: total=%d, used=%d, extents=%d", segId, segTotal2, segUsed2, segExtents2)

		// These should match the original values
		r.Equal(segTotal, segTotal2, "segment total should be preserved after rebuild")
		r.Equal(segUsed, segUsed2, "segment used should be preserved after rebuild")
		r.Equal(segExtents, segExtents2, "segment extent count should be preserved after rebuild")

		// Verify UsedBlocks returns the correct total
		totalUsed := d2.s.UsedBlocks()
		t.Logf("Total used blocks across all segments: %d", totalUsed)
		r.Equal(uint64(expectedUsedBlocks), totalUsed, "UsedBlocks should return correct total after rebuild")
	})

	t.Run("preserves segment usage through LBA map cache save and load", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		// Write some extents with known size
		extents := []struct {
			lba    LBA
			blocks int
		}{
			{0, 100},
			{200, 50},
		}

		for _, ext := range extents {
			data := make([]byte, ext.blocks*int(BlockSize))
			err := d.WriteExtent(ctx, BlockDataView(data).MapTo(ext.lba))
			r.NoError(err)
		}

		// Total = 150 blocks, Used = 150 blocks (no overlaps)
		expectedTotal := uint64(150)
		expectedUsed := uint64(150)

		err = d.CloseSegment(ctx)
		r.NoError(err)

		// Get segment ID
		segments := d.s.LiveSegments()
		r.Len(segments, 1)
		segId := segments[0]

		// Verify before save
		total1, used1, _ := d.s.SegmentInfo(segId)
		t.Logf("Before save - total=%d, used=%d", total1, used1)
		r.Equal(expectedTotal, total1)
		r.Equal(expectedUsed, used1)

		// Save the LBA map
		err = d.saveLBAMap(ctx)
		r.NoError(err)

		// Close and reopen - this should load from the cached LBA map
		d.Close(ctx)

		d2, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d2.Close(ctx)

		// Verify after load - the values should be preserved correctly
		total2, used2, _ := d2.s.SegmentInfo(segId)
		t.Logf("After load from cache - total=%d, used=%d", total2, used2)
		r.Equal(expectedTotal, total2, "total should be preserved through cache save/load")
		r.Equal(expectedUsed, used2, "used should be preserved through cache save/load")

		// Verify the total used blocks is also correct
		totalUsed := d2.s.UsedBlocks()
		r.Equal(expectedUsed, totalUsed, "UsedBlocks should return correct total")
	})

	t.Run("serializes the lba to pba mapping", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		err = d.CloseSegment(ctx)
		r.NoError(err)

		sh, err := d.segmentsHash(ctx)
		r.NoError(err)

		r.NoError(d.saveLBAMap(ctx))

		f, err := os.Open(filepath.Join(tmpdir, "head.map"))
		r.NoError(err)

		defer f.Close()

		m, hdr, err := processLBAMap(log, f)
		r.NoError(err)

		_, ok := m.m.Get(47)
		r.True(ok)

		r.Equal(sh, hdr.SegmentsHash)
	})

	t.Run("reuses serialized lba to pba map on start", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		d2, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)

		r.NotZero(d2.lba2pba.Len())
	})

	t.Run("replays logs into l2p map if need be on load", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(47))
		r.NoError(err)

		err = d.CloseSegment(ctx)
		r.NoError(err)

		r.NoError(d.saveLBAMap(ctx))

		r.NoError(d.WriteExtent(ctx, testExtent2.MapTo(48)))

		t.Log("reloading disk hot")

		d.er.Close()

		disk2, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer disk2.Close(ctx)

		d2, err := disk2.ReadExtent(ctx, Extent{48, 1})
		r.NoError(err)

		blockEqual(t, testExtent2, d2.ReadData())
	})

	t.Run("with multiple blocks", func(t *testing.T) {
		t.Run("writes are returned by next read", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			data := NewRangeData(ctx, Extent{0, 2})
			ds := data.WriteData()
			copy(ds, testData)
			copy(ds[BlockSize:], testData)

			err = d.WriteExtent(ctx, data)
			r.NoError(err)

			d2, err := d.ReadExtent(ctx, Extent{LBA: 1, Blocks: 1})
			r.NoError(err)

			blockEqual(t, d2.RawBlocks().BlockView(0), testData)

			d3, err := d.ReadExtent(ctx, Extent{LBA: 1, Blocks: 1})
			r.NoError(err)

			blockEqual(t, d3.RawBlocks().BlockView(0), testData)
		})

		t.Run("reads can return multiple blocks", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			data := NewRangeData(ctx, Extent{0, 2})
			copy(data.RawBlocks().BlockView(0), testData)
			copy(data.RawBlocks().BlockView(1), testData)

			err = d.WriteExtent(ctx, data)
			r.NoError(err)

			d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 2})
			r.NoError(err)

			blockEqual(t, d2.RawBlocks().BlockView(0), testData)
			blockEqual(t, d2.RawBlocks().BlockView(1), testData)

			d3, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			blockEqual(t, d3.RawBlocks().BlockView(0), testData)
			blockEqual(t, d2.RawBlocks().BlockView(1), testData)
		})

	})

	t.Run("writes to the same block return the most recent", func(t *testing.T) {
		t.Run("in the same instance", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			err = d.WriteExtent(ctx, testExtent.MapTo(0))
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent2.MapTo(0))
			r.NoError(err)

			d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testExtent2, d2)
		})

		t.Run("in a different instance", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			err = d.WriteExtent(ctx, testExtent.MapTo(0))
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent2.MapTo(0))
			r.NoError(err)

			r.NoError(d.Close(ctx))

			t.Log("reopening disk")

			d2, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d2.Close(ctx)

			x2, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testExtent2, x2)
		})

		t.Run("in a when recovering active", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent.MapTo(0))
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent2.MapTo(0))
			r.NoError(err)

			d.er.Close()

			d2, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d2.Close(ctx)

			x2, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testExtent2, x2)
		})

		t.Run("across segments", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			err = d.WriteExtent(ctx, testExtent.MapTo(0))
			r.NoError(err)

			err = d.CloseSegment(ctx)
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent2.MapTo(0))
			r.NoError(err)

			r.NoError(d.Close(ctx))

			d2, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d2.Close(ctx)

			x2, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testExtent2, x2)
		})

		t.Run("across segments without a lba map", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			err = d.WriteExtent(ctx, testExtent.MapTo(0))
			r.NoError(err)

			err = d.CloseSegment(ctx)
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent2.MapTo(0))
			r.NoError(err)

			r.NoError(d.Close(ctx))

			r.NoError(os.Remove(filepath.Join(tmpdir, "head.map")))

			d2, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d2.Close(ctx)

			x2, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testExtent2, x2)
		})

		t.Run("across and within segments without a lba map", func(t *testing.T) {
			r := require.New(t)

			tmpdir, err := os.MkdirTemp("", "lsvd")
			r.NoError(err)
			defer os.RemoveAll(tmpdir)

			d, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d.Close(ctx)

			err = d.WriteExtent(ctx, testExtent.MapTo(0))
			r.NoError(err)

			err = d.CloseSegment(ctx)
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent2.MapTo(0))
			r.NoError(err)

			err = d.WriteExtent(ctx, testExtent3.MapTo(0))
			r.NoError(err)

			r.NoError(d.Close(ctx))

			r.NoError(os.Remove(filepath.Join(tmpdir, "head.map")))

			d2, err := NewDisk(ctx, log, tmpdir)
			r.NoError(err)
			defer d2.Close(ctx)

			x2, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
			r.NoError(err)

			extentEqual(t, testExtent3, x2)
		})
	})

	t.Run("tracks segment usage data", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testExtent.MapTo(0))
		r.NoError(err)

		err = d.WriteExtent(ctx, testExtent2.MapTo(1))
		r.NoError(err)

		s1 := SegmentId(d.curSeq)
		err = d.CloseSegment(ctx)
		r.NoError(err)

		r.Len(d.s.segments, 1)

		stats, ok := d.s.segments[s1]
		r.True(ok)

		r.Equal(uint64(2), stats.Used)

		err = d.WriteExtent(ctx, testExtent3.MapTo(0))
		r.NoError(err)

		err = d.CloseSegment(ctx)
		r.NoError(err)

		r.Len(d.s.segments, 2)

		r.Equal(uint64(1), stats.Used)
	})

	t.Run("zero blocks works like an empty write", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		err = d.ZeroBlocks(ctx, Extent{0, 1})
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		//r.Nil(d2.ReadData(), "data shouldn't be allocated")
		//r.True(d2.EmptyP())

		data := d2.RawBlocks().BlockView(0)

		r.True(isEmpty(data))
	})

	t.Run("can use the write cache while currently uploading", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		var sa slowLocal
		sa.Log = log

		sa.Dir = tmpdir

		d, err := NewDisk(ctx, log, tmpdir, WithSegmentAccess(&sa))
		r.NoError(err)
		defer d.Close(ctx)

		sa.wait = make(chan struct{})
		defer close(sa.wait)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		_, err = d.closeSegmentAsync(ctx)
		r.NoError(err)

		time.Sleep(100 * time.Millisecond)

		r.True(sa.waiting.Load())

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d2)
	})

	t.Run("reads partly from both write caches and an segment", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		var sa slowLocal

		sa.Log = log
		sa.Dir = tmpdir

		d, err := NewDisk(ctx, log, tmpdir, WithSegmentAccess(&sa))
		r.NoError(err)
		defer d.Close(ctx)

		data := NewRangeData(ctx, Extent{0, 10})
		_, err = io.ReadFull(rand.Reader, data.WriteData())
		r.NoError(err)

		err = d.WriteExtent(ctx, data)
		r.NoError(err)

		err = d.CloseSegment(ctx)
		r.NoError(err)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		sa.wait = make(chan struct{})
		defer close(sa.wait)

		_, err = d.closeSegmentAsync(ctx)
		r.NoError(err)

		time.Sleep(100 * time.Millisecond)

		r.True(sa.waiting.Load())

		err = d.WriteExtent(ctx, testExtent.MapTo(1))
		r.NoError(err)

		t.Log("performing read")
		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 4})
		r.NoError(err)

		blockEqual(t, testRandX, d2.ReadData()[:BlockSize])
		blockEqual(t, testExtent, d2.ReadData()[BlockSize:BlockSize*2])
		blockEqual(t,
			data.ReadData()[BlockSize*2:BlockSize*4],
			d2.ReadData()[BlockSize*2:BlockSize*4],
		)
	})

	t.Run("supports writing multiple ranges at once", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtents(ctx, []RangeData{
			testRandX.MapTo(0),
			testRandX.MapTo(47),
		})
		r.NoError(err)

		d2, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d2)
		r.NoError(err)

		d3, err := d.ReadExtent(ctx, Extent{LBA: 47, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, d3)
		r.NoError(err)
	})

	t.Run("supports reading blocks from a read-only higher layer", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		// First, make the first layer
		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		// Reopen it to reinitialize it without any write caching bits.
		d, err = NewDisk(ctx, log, tmpdir, ReadOnly())
		r.NoError(err)
		defer d.Close(ctx)

		tmpdir2, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir2)

		// Now, the higher layer
		d2, err := NewDisk(ctx, log, tmpdir2,
			WithVolumeName("high"),
			WithLowerLayer(d),
		)
		r.NoError(err)
		defer d2.Close(ctx)

		pes, err := d2.lba2pba.Resolve(log, Extent{LBA: 0, Blocks: 1}, nil)
		r.NoError(err)

		r.Len(pes, 1)

		r.Equal(uint16(1), pes[0].Disk)

		data, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testRandX, data)
	})

	t.Run("supports reads from lowers in latest wins fashion", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		// First, make the first layer
		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		err = d.WriteExtent(ctx, testRandX.MapTo(0))
		r.NoError(err)

		r.NoError(d.Close(ctx))

		tmpdir3, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir3)

		// Second, make the second layer
		s, err := NewDisk(ctx, log, tmpdir3)
		r.NoError(err)
		defer s.Close(ctx)

		err = s.WriteExtent(ctx, testExtent.MapTo(0))
		r.NoError(err)

		r.NoError(s.Close(ctx))

		// Reopen it to reinitialize it without any write caching bits.
		d, err = NewDisk(ctx, log, tmpdir, ReadOnly())
		r.NoError(err)
		defer d.Close(ctx)

		s, err = NewDisk(ctx, log, tmpdir3, ReadOnly())
		r.NoError(err)
		defer s.Close(ctx)

		tmpdir2, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir2)

		// Now, the higher layer
		d2, err := NewDisk(ctx, log, tmpdir2,
			WithVolumeName("high"),
			WithLowerLayer(d),
			WithLowerLayer(s),
		)
		r.NoError(err)
		defer d2.Close(ctx)

		data, err := d2.ReadExtent(ctx, Extent{LBA: 0, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testExtent, data)
	})

	t.Run("supports reads from lowers that are different volumes of the same store", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		// First, make the first layer
		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		bd := NewRangeData(ctx, Extent{0, 4})
		_, err = io.ReadFull(rand.Reader, bd.WriteData())
		r.NoError(err)

		err = d.WriteExtent(ctx, bd)
		r.NoError(err)

		r.NoError(d.Close(ctx))

		tmpdir3, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		// Second, make the second layer
		s, err := NewDisk(ctx, log, tmpdir3,
			WithSegmentAccess(&LocalFileAccess{Dir: tmpdir, Log: log}),
			WithVolumeName("s"),
		)
		r.NoError(err)
		defer s.Close(ctx)

		// We'll write a replacement inside the range of d to be sure that
		// we observe the range splitting.
		err = s.WriteExtent(ctx, testExtent.MapTo(1))
		r.NoError(err)

		r.NoError(s.Close(ctx))

		// Reopen it to reinitialize it without any write caching bits.
		d, err = NewDisk(ctx, log, tmpdir, ReadOnly())
		r.NoError(err)
		defer d.Close(ctx)

		s, err = NewDisk(ctx, log, tmpdir3,
			WithSegmentAccess(&LocalFileAccess{Dir: tmpdir, Log: log}),
			WithVolumeName("s"),
			ReadOnly(),
		)
		r.NoError(err)
		defer s.Close(ctx)

		tmpdir2, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir2)

		// Now, the higher layer
		d2, err := NewDisk(ctx, log, tmpdir2,
			WithVolumeName("high"),
			WithLowerLayer(d),
			WithLowerLayer(s),
		)
		r.NoError(err)
		defer d2.Close(ctx)

		data, err := d2.ReadExtent(ctx, Extent{LBA: 1, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testExtent, data)

		// Make sure that the region of d that we created is still correct
		data2, err := d2.ReadExtent(ctx, Extent{LBA: 3, Blocks: 1})
		r.NoError(err)

		blockEqual(t, bd.ReadData()[BlockSize*3:], data2.ReadData())
	})

	t.Run("can pack all segments together", func(t *testing.T) {
		r := require.New(t)

		tmpdir, err := os.MkdirTemp("", "lsvd")
		r.NoError(err)
		defer os.RemoveAll(tmpdir)

		// First, make the first layer
		d, err := NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		bd := NewRangeData(ctx, Extent{0, 4})
		_, err = io.ReadFull(rand.Reader, bd.WriteData())
		r.NoError(err)

		err = d.WriteExtent(ctx, bd)
		r.NoError(err)

		err = d.WriteExtent(ctx, bd)
		r.NoError(err)

		data, err := d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 4})
		r.NoError(err)
		blockEqual(t, bd.ReadData(), data.ReadData())

		t.Log("closing segment")
		r.NoError(d.CloseSegment(ctx))

		t.Log("writing into new segment")
		err = d.WriteExtent(ctx, testExtent.MapTo(100))
		r.NoError(err)

		data, err = d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 4})
		r.NoError(err)
		blockEqual(t, bd.ReadData(), data.ReadData())

		t.Log("packing")
		r.NoError(d.Pack(ctx))

		r.Len(d.s.LiveSegments(), 1)

		r.NoError(d.Close(ctx))

		t.Log("reopening")
		d, err = NewDisk(ctx, log, tmpdir)
		r.NoError(err)
		defer d.Close(ctx)

		r.Len(d.s.LiveSegments(), 1)

		data, err = d.ReadExtent(ctx, Extent{LBA: 0, Blocks: 4})
		r.NoError(err)

		blockEqual(t, bd.ReadData(), data.ReadData())

		// Make sure that the region of d that we created is still correct
		data2, err := d.ReadExtent(ctx, Extent{LBA: 100, Blocks: 1})
		r.NoError(err)

		extentEqual(t, testExtent, data2)
	})

}

type slowVolume struct {
	Volume
	sl *slowLocal
}

var _ Volume = &slowVolume{}

func (s *slowVolume) NewSegment(ctx context.Context, seg SegmentId, layout *SegmentLayout, f *os.File) error {
	s.sl.waiting.Store(true)

	if s.sl.wait != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.sl.wait:
			// ok
		}
	}

	return s.Volume.NewSegment(ctx, seg, layout, f)
}

type slowLocal struct {
	LocalFileAccess
	waiting atomic.Bool
	wait    chan struct{}
}

func (s *slowLocal) OpenVolume(ctx context.Context, name string) (Volume, error) {
	vol, err := s.LocalFileAccess.OpenVolume(ctx, name)
	if err != nil {
		return nil, err
	}

	return &slowVolume{Volume: vol, sl: s}, nil
}

func (s *slowLocal) WriteSegment(ctx context.Context, seg SegmentId) (io.WriteCloser, error) {
	s.waiting.Store(true)

	if s.wait != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.wait:
			// ok
		}
	}

	return s.LocalFileAccess.WriteSegment(ctx, seg)
}

func (s *slowLocal) UploadSegment(ctx context.Context, seg SegmentId, f *os.File) error {
	s.waiting.Store(true)

	if s.wait != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wait:
			// ok
		}
	}

	return s.LocalFileAccess.UploadSegment(ctx, seg, f)
}

func (s *slowLocal) AppendSegment(ctx context.Context, vol string, seg SegmentId, f *os.File) error {
	s.waiting.Store(true)

	if s.wait != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wait:
			// ok
		}
	}

	return s.LocalFileAccess.AppendSegment(ctx, vol, seg, f)
}

func emptyBytesI(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}

	return true
}
func BenchmarkEmptyInline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		emptyBytesI(emptyBlock)
	}
}

func emptyBytes2(b []byte) bool {
	y := byte(0)
	for _, x := range b {
		y |= x
	}

	return y == 0
}

func BenchmarkEmptyInline2(b *testing.B) {
	for i := 0; i < b.N; i++ {
		emptyBytes2(emptyBlock)
	}
}

var local = make([]byte, BlockSize)

func BenchmarkEmptyEqual(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bytes.Equal(local, emptyBlock)
	}
}
