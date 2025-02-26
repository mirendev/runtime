package lsvd

import (
	"context"
	"fmt"
	"io"
	"os"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sys/unix"
)

type rangeCacheKey struct {
	Seg   SegmentId
	Chunk int64
}

type RangeCache struct {
	path  string
	f     *os.File
	chunk int64
	max   int64
	fetch func(ctx context.Context, seg SegmentId, data []byte, off int64) error

	lru *lru.Cache[rangeCacheKey, int64]

	chunkBuf []byte

	cacheRegion []byte
}

type RangeCacheOptions struct {
	Path      string
	ChunkSize int64
	MaxSize   int64
	Fetch     func(ctx context.Context, seg SegmentId, data []byte, off int64) error
}

func NewRangeCache(opts RangeCacheOptions) (*RangeCache, error) {
	f, err := os.Create(opts.Path)
	if err != nil {
		return nil, err
	}

	maxChunks := opts.MaxSize / opts.ChunkSize

	if maxChunks == 0 {
		return nil, fmt.Errorf("max size too small")
	}

	l, err := lru.New[rangeCacheKey, int64](int(maxChunks))
	if err != nil {
		return nil, err
	}

	fd := f.Fd()

	data, err := unix.Mmap(int(fd), 0, int(opts.MaxSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	rc := &RangeCache{
		path:  opts.Path,
		f:     f,
		chunk: opts.ChunkSize,
		max:   maxChunks,
		fetch: opts.Fetch,

		lru:      l,
		chunkBuf: make([]byte, opts.ChunkSize),

		cacheRegion: data,
	}

	return rc, nil
}

func (r *RangeCache) Close() error {
	if r.cacheRegion != nil {
		unix.Munmap(r.cacheRegion)
		r.cacheRegion = nil
	}

	if r.f != nil {
		r.f.Close()
		r.f = nil
	}

	return nil
}

func (r *RangeCache) ReadAt(ctx context.Context, seg SegmentId, buf []byte, off int64) (int, error) {
	firstChunk := off / r.chunk
	lastChunk := (off + int64(len(buf)) - 1) / r.chunk

	tot := len(buf)

	innerOff := off % r.chunk

	chunkData := r.chunkBuf

	for chunk := firstChunk; chunk <= lastChunk; chunk++ {
		ok, mem := r.memChunk(seg, chunk)

		if !ok {
			extentCacheMiss.Inc()

			err := r.fetch(ctx, seg, chunkData, chunk*r.chunk)
			if err != nil {
				return 0, fmt.Errorf("fetch failed: %w", err)
			}

			_, err = r.saveChunk(seg, chunk, chunkData)
			if err != nil {
				return 0, err
			}

			mem = chunkData
		} else {
			extentCacheHits.Inc()
		}

		copied := copy(buf, mem[innerOff:])

		if copied < len(buf) {
			buf = buf[copied:]
		}

		// Reset back because we want to read from the front of all future chunks
		innerOff = 0
	}

	return tot, nil
}

type CachePosition struct {
	fd   *os.File
	off  int64
	size int64
}

func (r *RangeCache) CachePositions(ctx context.Context, seg SegmentId, total, off int64, ret []CachePosition) ([]CachePosition, error) {
	firstChunk := off / r.chunk
	lastChunk := (off + total - 1) / r.chunk

	innerOff := off % r.chunk

	chunkData := r.chunkBuf

	left := total

	for chunk := firstChunk; chunk <= lastChunk; chunk++ {
		chunkOff := innerOff

		// it's possible r.chunk is wrong for the last chunk, but because we're only request
		// extents that fall within the chunk regardless, the bytes left will never run
		// into the area that isn't actually there.
		chunkLeft := r.chunk - chunkOff

		var consumed int64

		if chunkLeft >= left {
			consumed = left
		} else {
			consumed = chunkLeft
		}

		off, ok := r.lru.Get(rangeCacheKey{seg, chunk})
		if ok {
			extentCacheHits.Inc()
		} else {
			extentCacheMiss.Inc()

			err := r.fetch(ctx, seg, chunkData, chunk*r.chunk)
			if err != nil {
				return nil, err
			}

			off, err = r.saveChunk(seg, chunk, chunkData)
			if err != nil {
				return nil, err
			}
		}

		ret = append(ret, CachePosition{
			fd:   r.f,
			off:  off + innerOff,
			size: consumed,
		})

		left -= consumed

		// Reset back because we want to read from the front of all future chunks
		innerOff = 0
	}

	return ret, nil
}

func (r *RangeCache) memChunk(seg SegmentId, chunk int64) (bool, []byte) {
	off, ok := r.lru.Get(rangeCacheKey{seg, chunk})
	if !ok {
		return false, nil
	}

	return true, r.cacheRegion[off : off+r.chunk]
}

func (r *RangeCache) readChunk(seg SegmentId, chunk int64, data []byte) (bool, error) {
	off, ok := r.lru.Get(rangeCacheKey{seg, chunk})
	if !ok {
		return false, nil
	}

	n, err := r.f.ReadAt(data, off)
	if err != nil {
		return false, err
	}

	if n != len(data) {
		return false, io.ErrShortWrite
	}

	return true, nil
}

func (r *RangeCache) saveChunk(seg SegmentId, chunk int64, data []byte) (int64, error) {
	if r.lru.Len() < int(r.max) {
		off, err := r.f.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}

		n, err := r.f.Write(data)
		if err != nil {
			return 0, err
		}

		if n != len(data) {
			return 0, io.ErrShortWrite
		}

		r.lru.Add(rangeCacheKey{seg, chunk}, off)
		return off, nil
	}

	_, off, ok := r.lru.RemoveOldest()
	if !ok {
		return 0, fmt.Errorf("misused lru is empty")
	}

	n, err := r.f.WriteAt(data, off)
	if err != nil {
		return 0, err
	}

	if n != len(data) {
		return 0, io.ErrShortWrite
	}

	r.lru.Add(rangeCacheKey{seg, chunk}, off)

	return off, nil
}
