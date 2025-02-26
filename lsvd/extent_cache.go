package lsvd

import (
	"encoding/binary"

	"github.com/hashicorp/go-hclog"
	lru "github.com/hashicorp/golang-lru/v2"
	"go.etcd.io/bbolt"
)

type lruEntry struct {
	seg SegmentId
	off uint32
	ext Extent

	data []byte
}

type ExtentCache struct {
	log    hclog.Logger
	db     *bbolt.DB
	inUse  *lru.Cache[string, lruEntry]
	blocks int
}

const maxBlocks = 500000

var extentsBucket = []byte("extents")

func NewExtentCache(log hclog.Logger, path string) (*ExtentCache, error) {
	opts := bbolt.DefaultOptions
	db, err := bbolt.Open(path, 0644, opts)
	if err != nil {
		return nil, err
	}

	db.NoSync = true
	db.NoFreelistSync = true

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(extentsBucket)
		return err
	})
	if err != nil {
		return nil, err
	}

	iu, err := lru.New[string, lruEntry](1000)
	if err != nil {
		return nil, err
	}

	ec := &ExtentCache{
		log:   log,
		db:    db,
		inUse: iu,
	}

	err = ec.populateInUse()
	if err != nil {
		return nil, err
	}

	return ec, nil
}

func (e *ExtentCache) Close() error {
	return e.db.Close()
}

func (e *ExtentCache) parseKey(b []byte) (SegmentId, uint32, Extent) {
	seg := SegmentId(b[:SegmentIdSize])

	b = b[SegmentIdSize:]

	offset := binary.LittleEndian.Uint32(b)

	b = b[4:]

	var ext Extent
	ext.LBA = LBA(binary.LittleEndian.Uint64(b))
	ext.Blocks = binary.LittleEndian.Uint32(b[8:])

	return seg, offset, ext
}

func (e *ExtentCache) serializeKey(seg SegmentId, offset uint32, x Extent) []byte {
	var data [16 + SegmentIdSize]byte
	copy(data[:], seg[:])
	binary.LittleEndian.PutUint32(data[SegmentIdSize:], offset)
	binary.LittleEndian.PutUint64(data[SegmentIdSize+4:], uint64(x.LBA))
	binary.LittleEndian.PutUint32(data[SegmentIdSize+12:], uint32(x.Blocks))
	return data[:]
}

func (e *ExtentCache) populateInUse() error {
	return e.db.View(func(tx *bbolt.Tx) error {
		buk := tx.Bucket(extentsBucket)

		buk.ForEach(func(k, v []byte) error {
			seg, off, ext := e.parseKey(k)
			e.inUse.Add(string(k), lruEntry{seg, off, ext, v})
			return nil
		})
		return nil
	})
}

func (e *ExtentCache) makeRoom(buk *bbolt.Bucket, blks int) error {
	for e.blocks+blks > maxBlocks {
		if x, ent, ok := e.inUse.RemoveOldest(); ok {
			if err := buk.Delete([]byte(x)); err != nil {
				return err
			}

			e.blocks -= int(ent.ext.Blocks)
		} else {
			break
		}
	}

	return nil
}

func (e *ExtentCache) WriteExtent(robpb *PartialExtent, data []byte) error {
	ext := robpb.Extent
	seg := robpb.Segment
	off := robpb.Offset

	key := e.serializeKey(seg, off, ext)

	// dup := slices.Clone(data)
	//e.blocks += int(ext.Blocks)
	//e.inUse.Add(string(key), lruEntry{seg, off, ext, dup})

	//return nil

	return e.db.Update(func(tx *bbolt.Tx) error {
		buk := tx.Bucket(extentsBucket)

		if err := e.makeRoom(buk, int(ext.Blocks)); err != nil {
			return err
		}

		e.blocks += int(ext.Blocks)
		e.inUse.Add(string(key), lruEntry{seg, off, ext, nil})

		return buk.Put(key, data)
	})
}

func (e *ExtentCache) ReadExtent(robpb *PartialExtent, data []byte) (bool, error) {
	ext := robpb.Extent
	seg := robpb.Segment
	off := robpb.Offset

	key := e.serializeKey(seg, off, ext)

	ent, ok := e.inUse.Get(string(key))
	if !ok {
		return false, nil
	}

	if ent.data != nil {
		copy(data, ent.data)
		return true, nil
	}

	err := e.db.View(func(tx *bbolt.Tx) error {
		buk := tx.Bucket(extentsBucket)
		b := buk.Get(key)
		if b != nil {
			ok = true
			copy(data, b)

		}

		return nil
	})

	return ok, err
}
