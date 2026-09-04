// SPDX-License-Identifier: GPL-2.0
/*
 * LBD qcow2-lz4 backing store
 *
 * Implements a qcow2-inspired format with LZ4-compressed clusters
 * as the backing store for LBD block devices. Data is organized in
 * 64 KiB clusters with two-level (L1/L2) address translation.
 */

#include <linux/blkdev.h>
#include <linux/crc32c.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/highmem.h>
#include <linux/kernel.h>
#include <linux/slab.h>
#include <linux/byteorder/generic.h>

#include "lbd.h"
#include "lbd_qcow2.h"
#include "lz4_kcompat.h"

/* Forward declaration for base layer read (used by lbd_qcow2_cl_load) */
static int lbd_qcow2_base_read_cluster(struct lbd_device *dev,
					u64 cluster_index, void *buf);

/* ----------------------------------------------------------------
 * Helpers
 * ---------------------------------------------------------------- */

static inline u64 lbd_qcow2_lru_tick(struct lbd_qcow2 *q)
{
	return q->lru_tick++;
}

/* Write header's alloc_offset field to disk */
static int lbd_qcow2_write_alloc_offset(struct lbd_device *dev)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	u8 buf[8];
	loff_t pos = LBD_QCOW2_OFF_ALLOC_OFFSET;
	ssize_t ret;

	_qcow2_put64(buf, 0, q->alloc_offset);
	ret = kernel_write(dev->backing_file, buf, 8, &pos);
	if (ret != 8)
		return ret < 0 ? ret : -EIO;
	return 0;
}

/* Write header's free_list_head field to disk */
static int lbd_qcow2_write_free_list_head(struct lbd_device *dev)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	u8 buf[8];
	loff_t pos = LBD_QCOW2_OFF_FREE_LIST;
	ssize_t ret;

	_qcow2_put64(buf, 0, q->free_list_head);
	ret = kernel_write(dev->backing_file, buf, 8, &pos);
	if (ret != 8)
		return ret < 0 ? ret : -EIO;
	return 0;
}

/*
 * Compute the on-disk allocation size for an existing L2 entry.
 * Returns 0 on error.
 */
static u64 lbd_qcow2_read_old_alloc_size(struct lbd_device *dev, u64 l2_entry)
{
	struct lbd_qcow2 *q = &dev->qcow2;

	if (l2_entry == 0)
		return 0;

	if (l2_entry & LBD_QCOW2_L2_COMPRESSED) {
		u64 phys = l2_entry & LBD_QCOW2_L2_OFFSET_MASK;
		__be32 comp_size_be;
		u32 comp_size;
		loff_t pos = phys;
		ssize_t ret;

		ret = kernel_read(dev->backing_file, &comp_size_be,
				  sizeof(comp_size_be), &pos);
		if (ret != sizeof(comp_size_be))
			return 0;

		comp_size = be32_to_cpu(comp_size_be);
		return ALIGN(sizeof(__be32) + comp_size, 4096);
	}

	return q->cluster_size;
}

/*
 * Write a tombstone at a physical offset, prepending to the free list.
 * extent_size is the total usable space at that location.
 */
static int lbd_qcow2_free_extent(struct lbd_device *dev, loff_t phys_offset,
				  u64 extent_size)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	u8 buf[16];
	loff_t pos = phys_offset;
	ssize_t ret;

	/* Tombstone marker */
	_qcow2_put32(buf, 0, LBD_QCOW2_FREE_TOMBSTONE);
	/* Extent size */
	_qcow2_put32(buf, 4, (u32)extent_size);
	/* Next free pointer */
	_qcow2_put64(buf, 8, q->free_list_head);

	ret = kernel_write(dev->backing_file, buf, 16, &pos);
	if (ret != 16)
		return ret < 0 ? ret : -EIO;

	q->free_list_head = phys_offset;
	atomic64_inc(&dev->stat_alloc_freed);
	return lbd_qcow2_write_free_list_head(dev);
}

/* Maximum number of free list entries to scan when allocating */
#define LBD_QCOW2_FREE_SCAN_LIMIT	8

/* Minimum free entry size (tombstone header) */
#define LBD_QCOW2_FREE_ENTRY_MIN	16

/*
 * Try to allocate space from the free list or append.
 * needed: bytes required for the new on-disk data.
 * Returns the physical offset to write at via *out_phys.
 */
static int lbd_qcow2_alloc_space(struct lbd_device *dev, u64 needed,
				  loff_t *out_phys)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	loff_t prev_phys = 0;
	loff_t cur = q->free_list_head;
	int scanned = 0;
	int is_first = 1;

	while (cur != 0 && scanned < LBD_QCOW2_FREE_SCAN_LIMIT) {
		u8 buf[16];
		loff_t pos = cur;
		ssize_t ret;
		u32 tombstone, extent_size;
		u64 next_free;

		ret = kernel_read(dev->backing_file, buf, 16, &pos);
		if (ret != 16)
			break;

		tombstone = _qcow2_get32(buf, 0);
		if (tombstone != LBD_QCOW2_FREE_TOMBSTONE)
			break;

		extent_size = _qcow2_get32(buf, 4);
		next_free = _qcow2_get64(buf, 8);

		if (extent_size >= needed) {
			u64 remainder = extent_size - needed;

			/* Unlink this entry from the free list */
			if (is_first) {
				q->free_list_head = next_free;
			} else {
				/* Update previous entry's next pointer */
				u8 nbuf[8];
				loff_t npos = prev_phys + 8;

				_qcow2_put64(nbuf, 0, next_free);
				ret = kernel_write(dev->backing_file, nbuf, 8,
						   &npos);
				if (ret != 8)
					goto append;
			}

			/* Split if remainder is large enough */
			if (remainder >= LBD_QCOW2_FREE_ENTRY_MIN) {
				loff_t split_phys = cur + needed;
				int err;

				/*
				 * Add the remainder back as a new free entry
				 * at the head of the free list.
				 */
				err = lbd_qcow2_free_extent(dev, split_phys,
							    remainder);
				if (err) {
					/*
					 * Non-fatal: we waste the remainder
					 * but the allocation itself is fine.
					 */
					lbd_qcow2_write_free_list_head(dev);
				}
			} else {
				lbd_qcow2_write_free_list_head(dev);
			}

			*out_phys = cur;
			atomic64_inc(&dev->stat_alloc_reused);
			return 0;
		}

		prev_phys = cur;
		cur = next_free;
		is_first = 0;
		scanned++;
	}

append:
	*out_phys = q->alloc_offset;
	q->alloc_offset += needed;
	atomic64_inc(&dev->stat_alloc_new);
	return 0;
}

/* ----------------------------------------------------------------
 * L2 cache
 * ---------------------------------------------------------------- */

/* Find or load an L2 table into cache, return pointer to cache entry */
static struct lbd_l2_cache_entry *
lbd_qcow2_l2_get(struct lbd_device *dev, u32 l1_index)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct lbd_l2_cache_entry *best = NULL;
	u64 oldest = U64_MAX;
	int i;

	/* Check cache for hit */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &q->l2_cache[i];

		if (e->valid && e->l1_index == l1_index) {
			e->lru = lbd_qcow2_lru_tick(q);
			return e;
		}
	}

	/* Cache miss: find LRU entry to evict */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &q->l2_cache[i];

		if (!e->valid) {
			best = e;
			break;
		}
		if (e->lru < oldest) {
			oldest = e->lru;
			best = e;
		}
	}

	/* Flush dirty entry before evicting */
	if (best->valid && best->dirty) {
		u64 l2_phys = q->l1_table[best->l1_index];
		if (l2_phys) {
			loff_t pos = l2_phys;
			__be64 *disk_l2;
			int j;
			ssize_t ret;

			disk_l2 = kvmalloc(q->cluster_size, GFP_NOIO);
			if (disk_l2) {
				u8 *raw = (u8 *)disk_l2;
				u32 crc;
				__be32 crc_be;

				memset(disk_l2, 0, q->cluster_size);
				for (j = 0; j < q->l2_entries; j++)
					disk_l2[j] = cpu_to_be64(best->table[j]);

				/* CRC32C trailer */
				crc = ~crc32c(~0, raw, q->cluster_size - 4);
				crc_be = cpu_to_be32(crc);
				memcpy(raw + q->cluster_size - 4, &crc_be, 4);

				ret = kernel_write(dev->backing_file, disk_l2,
						   q->cluster_size, &pos);
				if (ret != q->cluster_size)
					pr_warn("lbd%d: L2 flush failed\n",
						dev->index);
				kvfree(disk_l2);
			}
		}
		best->dirty = false;
	}

	/* Load L2 table from disk */
	best->l1_index = l1_index;
	best->dirty = false;
	best->lru = lbd_qcow2_lru_tick(q);

	if (l1_index < q->l1_size && q->l1_table[l1_index] != 0) {
		loff_t pos = q->l1_table[l1_index];
		__be64 *disk_l2;
		ssize_t ret;
		int j;

		disk_l2 = kvmalloc(q->cluster_size, GFP_NOIO);
		if (!disk_l2) {
			best->valid = false;
			return NULL;
		}

		ret = kernel_read(dev->backing_file, disk_l2,
				  q->cluster_size, &pos);
		if (ret != q->cluster_size) {
			pr_warn("lbd%d: L2 read failed for l1[%u]\n",
				dev->index, l1_index);
			kvfree(disk_l2);
			best->valid = false;
			return NULL;
		}

		/* Verify CRC32C trailer */
		{
			u8 *raw = (u8 *)disk_l2;
			u32 stored_crc = _qcow2_get32(raw, q->cluster_size - 4);
			u32 calc_crc = ~crc32c(~0, raw, q->cluster_size - 4);

			if (stored_crc != calc_crc) {
				pr_warn("lbd%d: L2 CRC32C mismatch for l1[%u]: "
					"stored=0x%08x computed=0x%08x\n",
					dev->index, l1_index,
					stored_crc, calc_crc);
				kvfree(disk_l2);
				best->valid = false;
				return NULL;
			}
		}

		for (j = 0; j < q->l2_entries; j++)
			best->table[j] = be64_to_cpu(disk_l2[j]);

		kvfree(disk_l2);
	} else {
		/* Unallocated L2: all zeros */
		memset(best->table, 0, q->cluster_size);
	}

	best->valid = true;
	return best;
}

/* Flush a specific dirty L2 entry to disk */
static int lbd_qcow2_l2_flush(struct lbd_device *dev,
			       struct lbd_l2_cache_entry *e)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	u64 l2_phys;
	__be64 *disk_l2;
	loff_t pos;
	ssize_t ret;
	int j;

	if (!e->valid || !e->dirty)
		return 0;

	l2_phys = q->l1_table[e->l1_index];
	if (!l2_phys)
		return -EIO; /* should not happen */

	disk_l2 = kvmalloc(q->cluster_size, GFP_NOIO);
	if (!disk_l2)
		return -ENOMEM;

	memset(disk_l2, 0, q->cluster_size);
	for (j = 0; j < q->l2_entries; j++)
		disk_l2[j] = cpu_to_be64(e->table[j]);

	/* Compute and store CRC32C trailer at end of cluster */
	{
		u8 *raw = (u8 *)disk_l2;
		u32 crc = ~crc32c(~0, raw, q->cluster_size - 4);
		__be32 crc_be = cpu_to_be32(crc);

		memcpy(raw + q->cluster_size - 4, &crc_be, 4);
	}

	pos = l2_phys;
	ret = kernel_write(dev->backing_file, disk_l2, q->cluster_size, &pos);
	kvfree(disk_l2);

	if (ret != q->cluster_size)
		return ret < 0 ? ret : -EIO;

	e->dirty = false;
	return 0;
}

/* Allocate a new L2 table on disk if needed */
static int lbd_qcow2_l2_alloc(struct lbd_device *dev, u32 l1_index)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	loff_t pos;
	__be64 val;
	ssize_t ret;

	if (l1_index >= q->l1_size)
		return -ENOSPC;

	if (q->l1_table[l1_index] != 0)
		return 0; /* already allocated */

	/* Allocate cluster for L2 table at append point */
	q->l1_table[l1_index] = q->alloc_offset;
	q->alloc_offset += q->cluster_size;

	/* Write zeroed L2 table to disk (with CRC32C trailer) */
	{
		u8 *zeros = kvmalloc(q->cluster_size, GFP_NOIO);
		u32 crc;
		__be32 crc_be;

		if (!zeros)
			return -ENOMEM;
		memset(zeros, 0, q->cluster_size);

		/* CRC32C of all-zero data (covers bytes [0, cluster_size-4)) */
		crc = ~crc32c(~0, zeros, q->cluster_size - 4);
		crc_be = cpu_to_be32(crc);
		memcpy(zeros + q->cluster_size - 4, &crc_be, 4);

		pos = q->l1_table[l1_index];
		ret = kernel_write(dev->backing_file, zeros,
				   q->cluster_size, &pos);
		kvfree(zeros);
		if (ret != q->cluster_size)
			return ret < 0 ? ret : -EIO;
	}

	/* Write L1 entry to disk */
	val = cpu_to_be64(q->l1_table[l1_index]);
	pos = q->l1_offset + (loff_t)l1_index * sizeof(__be64);
	ret = kernel_write(dev->backing_file, &val, sizeof(val), &pos);
	if (ret != sizeof(val))
		return ret < 0 ? ret : -EIO;

	return 0;
}

/* ----------------------------------------------------------------
 * Cluster cache
 * ---------------------------------------------------------------- */

static struct lbd_cl_cache_entry *
lbd_qcow2_cl_find(struct lbd_qcow2 *q, u64 cluster_index)
{
	int i;

	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++) {
		struct lbd_cl_cache_entry *e = &q->cl_cache[i];

		if (e->valid && e->cluster_index == cluster_index) {
			e->lru = lbd_qcow2_lru_tick(q);
			return e;
		}
	}
	return NULL;
}

static void lbd_qcow2_cl_invalidate(struct lbd_qcow2 *q, u64 cluster_index)
{
	int i;

	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++) {
		struct lbd_cl_cache_entry *e = &q->cl_cache[i];

		if (e->valid && e->cluster_index == cluster_index) {
			e->valid = false;
			return;
		}
	}
}

static struct lbd_cl_cache_entry *
lbd_qcow2_cl_alloc_entry(struct lbd_qcow2 *q)
{
	struct lbd_cl_cache_entry *best = NULL;
	u64 oldest = U64_MAX;
	int i;

	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++) {
		struct lbd_cl_cache_entry *e = &q->cl_cache[i];

		if (!e->valid) {
			best = e;
			break;
		}
		if (e->lru < oldest) {
			oldest = e->lru;
			best = e;
		}
	}

	/* Evict LRU (cluster cache is read-only cache, no flush needed) */
	best->valid = false;
	return best;
}

/*
 * Return a zeroed cache entry for write path when cluster is unallocated.
 * Caller holds q->rwsem for write.
 */
static struct lbd_cl_cache_entry *
lbd_qcow2_cl_get_zero(struct lbd_device *dev, u64 cluster_index)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct lbd_cl_cache_entry *ce;

	ce = lbd_qcow2_cl_find(q, cluster_index);
	if (ce)
		return ce;

	ce = lbd_qcow2_cl_alloc_entry(q);
	ce->cluster_index = cluster_index;
	ce->lru = lbd_qcow2_lru_tick(q);
	ce->dirty = false;
	memset(ce->data, 0, q->cluster_size);
	ce->valid = true;
	return ce;
}

/* Load a cluster into cache from disk via L2 lookup */
static struct lbd_cl_cache_entry *
lbd_qcow2_cl_load(struct lbd_device *dev, u64 cluster_index)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct lbd_cl_cache_entry *ce;
	struct lbd_l2_cache_entry *l2e;
	u32 l1_idx, l2_idx;
	u64 l2_entry, phys_offset;
	ssize_t ret;

	/* Check cache first */
	ce = lbd_qcow2_cl_find(q, cluster_index);
	if (ce)
		return ce;

	/* L2 lookup */
	l1_idx = cluster_index / q->l2_entries;
	l2_idx = cluster_index % q->l2_entries;

	l2e = lbd_qcow2_l2_get(dev, l1_idx);
	if (!l2e)
		return NULL;

	l2_entry = l2e->table[l2_idx];

	/* Get a cache entry */
	ce = lbd_qcow2_cl_alloc_entry(q);
	ce->cluster_index = cluster_index;
	ce->lru = lbd_qcow2_lru_tick(q);
	ce->dirty = false;

	if (l2_entry == 0) {
		if (dev->base) {
			int err = lbd_qcow2_base_read_cluster(dev,
					cluster_index, ce->data);
			if (err == 1) {
				/* Unallocated in both primary and base */
#ifdef CONFIG_LBD_MISS_HANDLER
				if (dev->miss_handler)
					return ERR_PTR(-ENODATA);
#endif
				/* No handler: zero-fill (backward compatible) */
				memset(ce->data, 0, q->cluster_size);
			} else if (err < 0) {
				return NULL; /* I/O error */
			}
			/* err == 0: data was read successfully */
#ifdef CONFIG_LBD_MISS_HANDLER
		} else if (dev->miss_handler) {
			/* No base, but miss handler registered */
			return ERR_PTR(-ENODATA);
#endif
		} else {
			memset(ce->data, 0, q->cluster_size);
		}
		ce->valid = true;
		return ce;
	}

	phys_offset = l2_entry & LBD_QCOW2_L2_OFFSET_MASK;

	if (l2_entry & LBD_QCOW2_L2_COMPRESSED) {
		/* Compressed cluster: read size header + compressed data */
		__be32 comp_size_be;
		u32 comp_size;
		int dec_len;
		loff_t pos = phys_offset;

		ret = kernel_read(dev->backing_file, &comp_size_be,
				  sizeof(comp_size_be), &pos);
		if (ret != sizeof(comp_size_be)) {
			pr_warn("lbd%d: failed to read compressed size\n",
				dev->index);
			return NULL;
		}

		comp_size = be32_to_cpu(comp_size_be);
		if (comp_size > LZ4_compressBound(q->cluster_size)) {
			pr_warn("lbd%d: invalid compressed size %u\n",
				dev->index, comp_size);
			return NULL;
		}

		ret = kernel_read(dev->backing_file, q->read_buf,
				  comp_size, &pos);
		if (ret != comp_size) {
			pr_warn("lbd%d: failed to read compressed data\n",
				dev->index);
			return NULL;
		}

		dec_len = LZ4_decompress_safe(q->read_buf, ce->data,
					      comp_size, q->cluster_size);
		if (dec_len != q->cluster_size) {
			pr_warn("lbd%d: LZ4 decompress failed (%d)\n",
				dev->index, dec_len);
			return NULL;
		}
	} else {
		/* Uncompressed cluster */
		loff_t pos = phys_offset;

		ret = kernel_read(dev->backing_file, ce->data,
				  q->cluster_size, &pos);
		if (ret != q->cluster_size) {
			pr_warn("lbd%d: failed to read cluster\n",
				dev->index);
			return NULL;
		}
	}

	ce->valid = true;
	return ce;
}

/*
 * Flush all dirty L2 cache entries to disk.
 */
static void lbd_qcow2_flush_all_l2(struct lbd_device *dev)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	int i;

	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &q->l2_cache[i];
		if (e->valid && e->dirty)
			lbd_qcow2_l2_flush(dev, e);
	}
}

/* ----------------------------------------------------------------
 * Init / Destroy
 * ---------------------------------------------------------------- */

int lbd_qcow2_init(struct lbd_device *dev)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	u8 *hdr;
	loff_t pos = 0;
	ssize_t ret;
	__be64 *disk_l1;
	int i;

	hdr = kvmalloc(LBD_QCOW2_HEADER_SIZE, GFP_KERNEL);
	if (!hdr)
		return -ENOMEM;

	/* Read header */
	ret = kernel_read(dev->backing_file, hdr, LBD_QCOW2_HEADER_SIZE, &pos);
	if (ret != LBD_QCOW2_HEADER_SIZE) {
		pr_err("lbd%d: qcow2 header read failed\n", dev->index);
		kvfree(hdr);
		return ret < 0 ? ret : -EIO;
	}

	if (lbd_qcow2_hdr_magic(hdr) != LBD_QCOW2_MAGIC) {
		pr_err("lbd%d: invalid qcow2 magic\n", dev->index);
		kvfree(hdr);
		return -EINVAL;
	}

	if (lbd_qcow2_hdr_version(hdr) != LBD_QCOW2_VERSION) {
		pr_err("lbd%d: unsupported qcow2 version %u\n",
		       dev->index, lbd_qcow2_hdr_version(hdr));
		kvfree(hdr);
		return -EINVAL;
	}

	q->cluster_bits = lbd_qcow2_hdr_cluster_bits(hdr);
	if (q->cluster_bits < 12 || q->cluster_bits > 24) {
		pr_err("lbd%d: invalid cluster_bits %u\n",
		       dev->index, q->cluster_bits);
		kvfree(hdr);
		return -EINVAL;
	}

	q->cluster_size = 1U << q->cluster_bits;
	q->l2_entries = (q->cluster_size - LBD_QCOW2_L2_TRAILER_SIZE) / sizeof(u64);
	q->virtual_size = lbd_qcow2_hdr_virtual_size(hdr);
	q->l1_offset = lbd_qcow2_hdr_l1_table_offset(hdr);
	q->l1_size = lbd_qcow2_hdr_l1_size(hdr);
	q->alloc_offset = lbd_qcow2_hdr_alloc_offset(hdr);
	q->free_list_head = lbd_qcow2_hdr_free_list(hdr);
	q->lru_tick = 0;

	kvfree(hdr);

	init_rwsem(&q->rwsem);

	/* Allocate and read L1 table */
	q->l1_table = kvmalloc_array(q->l1_size, sizeof(u64), GFP_KERNEL);
	if (!q->l1_table)
		return -ENOMEM;

	disk_l1 = kvmalloc_array(q->l1_size, sizeof(__be64), GFP_KERNEL);
	if (!disk_l1) {
		kvfree(q->l1_table);
		q->l1_table = NULL;
		return -ENOMEM;
	}

	pos = q->l1_offset;
	ret = kernel_read(dev->backing_file, disk_l1,
			  q->l1_size * sizeof(__be64), &pos);
	if (ret != q->l1_size * sizeof(__be64)) {
		pr_err("lbd%d: L1 table read failed\n", dev->index);
		kvfree(disk_l1);
		kvfree(q->l1_table);
		q->l1_table = NULL;
		return ret < 0 ? ret : -EIO;
	}

	for (i = 0; i < q->l1_size; i++)
		q->l1_table[i] = be64_to_cpu(disk_l1[i]);
	kvfree(disk_l1);

	/* Allocate L2 cache entries */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &q->l2_cache[i];

		e->table = kvmalloc(q->cluster_size, GFP_KERNEL);
		if (!e->table)
			goto err_l2_cache;
		e->valid = false;
		e->dirty = false;
	}

	/* Allocate cluster cache entries */
	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++) {
		struct lbd_cl_cache_entry *e = &q->cl_cache[i];

		e->data = kvmalloc(q->cluster_size, GFP_KERNEL);
		if (!e->data)
			goto err_cl_cache;
		e->valid = false;
		e->dirty = false;
	}

	/* Allocate compression buffer */
	q->comp_buf = kvmalloc(LZ4_compressBound(q->cluster_size), GFP_KERNEL);
	if (!q->comp_buf)
		goto err_cl_cache;

	/* Allocate read buffer for compressed data */
	q->read_buf = kvmalloc(LZ4_compressBound(q->cluster_size), GFP_KERNEL);
	if (!q->read_buf) {
		kvfree(q->comp_buf);
		q->comp_buf = NULL;
		goto err_cl_cache;
	}

	pr_info("lbd%d: qcow2-lz4 format detected, virtual_size=%llu, "
		"cluster_size=%u, l1_size=%u\n",
		dev->index, q->virtual_size, q->cluster_size, q->l1_size);

	return 0;

err_cl_cache:
	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++)
		kvfree(q->cl_cache[i].data);
err_l2_cache:
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++)
		kvfree(q->l2_cache[i].table);
	kvfree(q->l1_table);
	q->l1_table = NULL;
	return -ENOMEM;
}

void lbd_qcow2_destroy(struct lbd_device *dev)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	int i;

	/* Flush any dirty L2 entries */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &q->l2_cache[i];

		if (e->valid && e->dirty)
			lbd_qcow2_l2_flush(dev, e);
	}

	/* Write final alloc_offset and free_list_head */
	lbd_qcow2_write_alloc_offset(dev);
	lbd_qcow2_write_free_list_head(dev);

	kvfree(q->read_buf);
	kvfree(q->comp_buf);

	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++)
		kvfree(q->cl_cache[i].data);
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++)
		kvfree(q->l2_cache[i].table);

	kvfree(q->l1_table);
	q->l1_table = NULL;
}

#ifdef CONFIG_LBD_MISS_HANDLER
/* ----------------------------------------------------------------
 * Miss handler support
 * ---------------------------------------------------------------- */

/* Declared in lbd.c, defined there to access miss handler internals */
enum lbd_miss_action lbd_qcow2_handle_miss(struct lbd_device *dev,
					    u64 cluster_index);

/*
 * Invalidate cached entries for a cluster before retry.
 * Called with rwsem released.
 */
static void lbd_qcow2_invalidate_for_retry(struct lbd_device *dev,
					    u64 cluster_index)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct lbd_qcow2_base *base = dev->base;
	int i;

	/* Invalidate primary cluster cache */
	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++) {
		struct lbd_cl_cache_entry *e = &q->cl_cache[i];

		if (e->valid && e->cluster_index == cluster_index)
			e->valid = false;
	}

	/* Invalidate base L2 cache entry covering this cluster */
	if (base && base->is_qcow2) {
		u32 l1_idx = cluster_index / base->l2_entries;

		for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
			struct lbd_l2_cache_entry *e = &base->l2_cache[i];

			if (e->valid && e->l1_index == l1_idx)
				e->valid = false;
		}
	}
}
#endif /* CONFIG_LBD_MISS_HANDLER */

/* ----------------------------------------------------------------
 * Read path
 * ---------------------------------------------------------------- */

int lbd_qcow2_read(struct lbd_device *dev, struct request *rq)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct req_iterator iter;
	struct bio_vec bvec;
	loff_t guest_offset = (loff_t)blk_rq_pos(rq) << SECTOR_SHIFT;

	rq_for_each_segment(bvec, rq, iter) {
		void *mapped;
		unsigned int remaining = bvec.bv_len;
		unsigned int bv_off = bvec.bv_offset;

		mapped = kmap_local_page(bvec.bv_page);

		while (remaining > 0) {
			u64 cluster_idx = guest_offset >> q->cluster_bits;
			u32 off_in_cluster = guest_offset & (q->cluster_size - 1);
			u32 bytes = min_t(u32, remaining,
					  q->cluster_size - off_in_cluster);
			struct lbd_cl_cache_entry *ce;
#ifdef CONFIG_LBD_MISS_HANDLER
			int retries = 0;

retry_cluster:
#endif
			down_read(&q->rwsem);

			ce = lbd_qcow2_cl_load(dev, cluster_idx);
#ifdef CONFIG_LBD_MISS_HANDLER
			if (IS_ERR(ce)) {
				enum lbd_miss_action action;

				up_read(&q->rwsem);
				/* -ENODATA = miss, ask userspace */
				action = lbd_qcow2_handle_miss(dev,
							       cluster_idx);
				if (action == LBD_MISS_RETRY &&
				    retries++ < 3) {
					lbd_qcow2_invalidate_for_retry(dev,
								       cluster_idx);
					goto retry_cluster;
				}
				/* CONTINUE or retry exhausted: zero-fill */
				memset(mapped + bv_off, 0, bytes);
				goto next_chunk;
			}
#endif
			if (!ce) {
				/* Real I/O error */
				up_read(&q->rwsem);
				kunmap_local(mapped);
				return -EIO;
			}

			memcpy(mapped + bv_off, ce->data + off_in_cluster,
			       bytes);

			up_read(&q->rwsem);

#ifdef CONFIG_LBD_MISS_HANDLER
next_chunk:
#endif
			bv_off += bytes;
			guest_offset += bytes;
			remaining -= bytes;
		}

		kunmap_local(mapped);
	}

	atomic64_inc(&dev->stat_reads);
	atomic64_add(blk_rq_bytes(rq), &dev->stat_read_bytes);
	return 0;
}

/* ----------------------------------------------------------------
 * Write path
 * ---------------------------------------------------------------- */

int lbd_qcow2_write(struct lbd_device *dev, struct request *rq)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct req_iterator iter;
	struct bio_vec bvec;
	loff_t guest_offset = (loff_t)blk_rq_pos(rq) << SECTOR_SHIFT;
	u32 total_len = blk_rq_bytes(rq);
	void *write_data;
	size_t offset = 0;

	/* Gather all write data into contiguous buffer */
	write_data = kvmalloc(total_len, GFP_NOIO);
	if (!write_data)
		return -ENOMEM;

	rq_for_each_segment(bvec, rq, iter) {
		void *mapped = kmap_local_page(bvec.bv_page);
		memcpy(write_data + offset, mapped + bvec.bv_offset,
		       bvec.bv_len);
		kunmap_local(mapped);
		offset += bvec.bv_len;
	}

	down_write(&q->rwsem);

	offset = 0;
	while (offset < total_len) {
		u64 cluster_idx = guest_offset >> q->cluster_bits;
		u32 off_in_cluster = guest_offset & (q->cluster_size - 1);
		u32 bytes = min_t(u32, total_len - offset,
				  q->cluster_size - off_in_cluster);
		struct lbd_cl_cache_entry *ce;
		struct lbd_l2_cache_entry *l2e;
		u32 l1_idx = cluster_idx / q->l2_entries;
		u32 l2_idx = cluster_idx % q->l2_entries;
		int comp_len;
		loff_t phys;
		ssize_t ret;
		int err;
		u64 old_l2_entry;
		u64 old_alloc, new_alloc;

		/* Ensure L2 table is allocated */
		err = lbd_qcow2_l2_alloc(dev, l1_idx);
		if (err) {
			up_write(&q->rwsem);
			kvfree(write_data);
			return err;
		}

		/* Load existing cluster into cache (or zeros if new) */
		ce = lbd_qcow2_cl_load(dev, cluster_idx);
		if (IS_ERR(ce)) {
			/*
			 * -ENODATA: cluster unallocated (miss).  For writes we
			 * don't need the old data — just grab a zeroed cache
			 * slot and let the write overwrite it.
			 */
			ce = lbd_qcow2_cl_get_zero(dev, cluster_idx);
			if (!ce) {
				up_write(&q->rwsem);
				kvfree(write_data);
				return -EIO;
			}
		} else if (!ce) {
			up_write(&q->rwsem);
			kvfree(write_data);
			return -EIO;
		}

		/* Read old L2 entry before modifying */
		l2e = lbd_qcow2_l2_get(dev, l1_idx);
		if (!l2e) {
			up_write(&q->rwsem);
			kvfree(write_data);
			return -EIO;
		}
		old_l2_entry = l2e->table[l2_idx];
		old_alloc = lbd_qcow2_read_old_alloc_size(dev, old_l2_entry);

		/* Apply write data to cluster */
		memcpy(ce->data + off_in_cluster, write_data + offset, bytes);

		/* Compress the full cluster */
		comp_len = LZ4_compress_fast_extState(
			dev->lz4_state, ce->data, q->comp_buf,
			q->cluster_size,
			LZ4_compressBound(q->cluster_size), 1);

		if (comp_len > 0 &&
		    (u32)comp_len < q->cluster_size - sizeof(__be32)) {
			/* Store compressed */
			__be32 comp_size_be = cpu_to_be32(comp_len);
			new_alloc = ALIGN(sizeof(__be32) + comp_len, 4096);

			if (old_alloc > 0 && new_alloc <= old_alloc) {
				phys = old_l2_entry & LBD_QCOW2_L2_OFFSET_MASK;
				atomic64_inc(&dev->stat_alloc_reused);
			} else {
				if (old_alloc > 0) {
					loff_t old_phys = old_l2_entry &
						LBD_QCOW2_L2_OFFSET_MASK;
					err = lbd_qcow2_free_extent(dev,
						old_phys, old_alloc);
					if (err) {
						up_write(&q->rwsem);
						kvfree(write_data);
						return err;
					}
				}
				err = lbd_qcow2_alloc_space(dev, new_alloc,
							    &phys);
				if (err) {
					up_write(&q->rwsem);
					kvfree(write_data);
					return err;
				}
			}

			/* Write size header + compressed data */
			{
				loff_t pos = phys;
				ret = kernel_write(dev->backing_file,
						   &comp_size_be,
						   sizeof(comp_size_be), &pos);
				if (ret != sizeof(comp_size_be)) {
					up_write(&q->rwsem);
					kvfree(write_data);
					return ret < 0 ? ret : -EIO;
				}

				ret = kernel_write(dev->backing_file,
						   q->comp_buf, comp_len,
						   &pos);
				if (ret != comp_len) {
					up_write(&q->rwsem);
					kvfree(write_data);
					return ret < 0 ? ret : -EIO;
				}
			}

			l2e->table[l2_idx] = LBD_QCOW2_L2_COMPRESSED | phys;
			l2e->dirty = true;
			atomic64_inc(&dev->stat_compressed);
		} else {
			/* Store uncompressed */
			new_alloc = q->cluster_size;

			if (old_alloc > 0 && new_alloc <= old_alloc) {
				phys = old_l2_entry & LBD_QCOW2_L2_OFFSET_MASK;
				atomic64_inc(&dev->stat_alloc_reused);
			} else {
				if (old_alloc > 0) {
					loff_t old_phys = old_l2_entry &
						LBD_QCOW2_L2_OFFSET_MASK;
					err = lbd_qcow2_free_extent(dev,
						old_phys, old_alloc);
					if (err) {
						up_write(&q->rwsem);
						kvfree(write_data);
						return err;
					}
				}
				err = lbd_qcow2_alloc_space(dev, new_alloc,
							    &phys);
				if (err) {
					up_write(&q->rwsem);
					kvfree(write_data);
					return err;
				}
			}

			{
				loff_t pos = phys;
				ret = kernel_write(dev->backing_file,
						   ce->data, q->cluster_size,
						   &pos);
				if (ret != q->cluster_size) {
					up_write(&q->rwsem);
					kvfree(write_data);
					return ret < 0 ? ret : -EIO;
				}
			}

			l2e->table[l2_idx] = phys;
			l2e->dirty = true;
			atomic64_inc(&dev->stat_uncompressed);
		}

		/* Flush L2 to disk */
		err = lbd_qcow2_l2_flush(dev, l2e);
		if (err) {
			up_write(&q->rwsem);
			kvfree(write_data);
			return err;
		}

		/* Update alloc_offset on disk */
		err = lbd_qcow2_write_alloc_offset(dev);
		if (err) {
			up_write(&q->rwsem);
			kvfree(write_data);
			return err;
		}

		offset += bytes;
		guest_offset += bytes;
	}

	up_write(&q->rwsem);
	kvfree(write_data);
	return 0;
}

/* ----------------------------------------------------------------
 * TRIM path
 * ---------------------------------------------------------------- */

int lbd_qcow2_discard(struct lbd_device *dev, struct request *rq)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	loff_t guest_offset = (loff_t)blk_rq_pos(rq) << SECTOR_SHIFT;
	u32 remaining = blk_rq_bytes(rq);
	int err;

	down_write(&q->rwsem);

	while (remaining > 0) {
		u64 cluster_idx = guest_offset >> q->cluster_bits;
		u32 off_in_cluster = guest_offset & (q->cluster_size - 1);
		u32 bytes = min_t(u32, remaining,
				  q->cluster_size - off_in_cluster);
		u32 l1_idx = cluster_idx / q->l2_entries;
		u32 l2_idx = cluster_idx % q->l2_entries;
		struct lbd_l2_cache_entry *l2e;

		if (off_in_cluster == 0 && bytes == q->cluster_size) {
			/* Full-cluster trim */
			if (l1_idx < q->l1_size &&
			    q->l1_table[l1_idx] != 0) {
				l2e = lbd_qcow2_l2_get(dev, l1_idx);
				if (l2e && l2e->table[l2_idx] != 0) {
					u64 old_l2 = l2e->table[l2_idx];
					u64 old_sz = lbd_qcow2_read_old_alloc_size(
							dev, old_l2);
					if (old_sz > 0) {
						loff_t old_phys = old_l2 &
							LBD_QCOW2_L2_OFFSET_MASK;
						lbd_qcow2_free_extent(dev,
							old_phys, old_sz);
					}
					l2e->table[l2_idx] = 0;
					l2e->dirty = true;
					err = lbd_qcow2_l2_flush(dev, l2e);
					if (err) {
						up_write(&q->rwsem);
						return err;
					}
				}
			}
			lbd_qcow2_cl_invalidate(q, cluster_idx);
		} else {
			/* Partial-cluster trim: read-modify-write */
			struct lbd_cl_cache_entry *ce;

			err = lbd_qcow2_l2_alloc(dev, l1_idx);
			if (err) {
				up_write(&q->rwsem);
				return err;
			}

			ce = lbd_qcow2_cl_load(dev, cluster_idx);
			if (IS_ERR(ce)) {
				/*
				 * -ENODATA: cluster unallocated.  Trimming
				 * an unallocated cluster is a no-op.
				 */
				goto next;
			}
			if (!ce) {
				up_write(&q->rwsem);
				return -EIO;
			}

			l2e = lbd_qcow2_l2_get(dev, l1_idx);
			if (!l2e) {
				up_write(&q->rwsem);
				return -EIO;
			}

			if (l2e->table[l2_idx] == 0) {
				goto next;
			}

			/* Zero the trimmed portion */
			memset(ce->data + off_in_cluster, 0, bytes);

			/* Recompress and write back */
			{
				int comp_len;
				loff_t phys;
				ssize_t ret;
				u64 old_l2 = l2e->table[l2_idx];
				u64 old_alloc = lbd_qcow2_read_old_alloc_size(
						dev, old_l2);
				u64 new_alloc;

				comp_len = LZ4_compress_fast_extState(
					dev->lz4_state, ce->data, q->comp_buf,
					q->cluster_size,
					LZ4_compressBound(q->cluster_size), 1);

				if (comp_len > 0 &&
				    (u32)comp_len < q->cluster_size - sizeof(__be32)) {
					__be32 comp_size_be = cpu_to_be32(comp_len);
					new_alloc = ALIGN(sizeof(__be32) + comp_len, 4096);

					if (old_alloc > 0 && new_alloc <= old_alloc) {
						phys = old_l2 & LBD_QCOW2_L2_OFFSET_MASK;
						atomic64_inc(&dev->stat_alloc_reused);
					} else {
						if (old_alloc > 0) {
							loff_t old_phys = old_l2 &
								LBD_QCOW2_L2_OFFSET_MASK;
							lbd_qcow2_free_extent(dev,
								old_phys, old_alloc);
						}
						err = lbd_qcow2_alloc_space(dev,
							new_alloc, &phys);
						if (err) {
							up_write(&q->rwsem);
							return err;
						}
					}

					{
						loff_t pos = phys;
						ret = kernel_write(dev->backing_file,
								   &comp_size_be,
								   sizeof(comp_size_be), &pos);
						if (ret != sizeof(comp_size_be)) {
							up_write(&q->rwsem);
							return ret < 0 ? ret : -EIO;
						}
						ret = kernel_write(dev->backing_file,
								   q->comp_buf, comp_len,
								   &pos);
						if (ret != comp_len) {
							up_write(&q->rwsem);
							return ret < 0 ? ret : -EIO;
						}
					}

					l2e->table[l2_idx] = LBD_QCOW2_L2_COMPRESSED | phys;
					atomic64_inc(&dev->stat_compressed);
				} else {
					new_alloc = q->cluster_size;

					if (old_alloc > 0 && new_alloc <= old_alloc) {
						phys = old_l2 & LBD_QCOW2_L2_OFFSET_MASK;
						atomic64_inc(&dev->stat_alloc_reused);
					} else {
						if (old_alloc > 0) {
							loff_t old_phys = old_l2 &
								LBD_QCOW2_L2_OFFSET_MASK;
							lbd_qcow2_free_extent(dev,
								old_phys, old_alloc);
						}
						err = lbd_qcow2_alloc_space(dev,
							new_alloc, &phys);
						if (err) {
							up_write(&q->rwsem);
							return err;
						}
					}

					{
						loff_t pos = phys;
						ret = kernel_write(dev->backing_file,
								   ce->data, q->cluster_size,
								   &pos);
						if (ret != q->cluster_size) {
							up_write(&q->rwsem);
							return ret < 0 ? ret : -EIO;
						}
					}

					l2e->table[l2_idx] = phys;
					atomic64_inc(&dev->stat_uncompressed);
				}

				l2e->dirty = true;
				err = lbd_qcow2_l2_flush(dev, l2e);
				if (err) {
					up_write(&q->rwsem);
					return err;
				}

				err = lbd_qcow2_write_alloc_offset(dev);
				if (err) {
					up_write(&q->rwsem);
					return err;
				}
			}
		}

next:
		guest_offset += bytes;
		remaining -= bytes;
	}

	up_write(&q->rwsem);
	return 0;
}

/* ----------------------------------------------------------------
 * Base layer (thin snapshot) support
 * ---------------------------------------------------------------- */

static inline u64 lbd_qcow2_base_lru_tick(struct lbd_qcow2_base *base)
{
	return base->lru_tick++;
}

/* Find or load an L2 table from the base layer into its cache */
static struct lbd_l2_cache_entry *
lbd_qcow2_base_l2_get(struct lbd_qcow2_base *base, u32 l1_index)
{
	struct lbd_l2_cache_entry *best = NULL;
	u64 oldest = U64_MAX;
	int i;

	/* Check cache for hit */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &base->l2_cache[i];

		if (e->valid && e->l1_index == l1_index) {
			e->lru = lbd_qcow2_base_lru_tick(base);
			return e;
		}
	}

	/* Cache miss: find LRU entry to evict */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
		struct lbd_l2_cache_entry *e = &base->l2_cache[i];

		if (!e->valid) {
			best = e;
			break;
		}
		if (e->lru < oldest) {
			oldest = e->lru;
			best = e;
		}
	}

	/* No dirty tracking — just overwrite on eviction */
	best->l1_index = l1_index;
	best->dirty = false;
	best->lru = lbd_qcow2_base_lru_tick(base);

	if (l1_index < base->l1_size && base->l1_table[l1_index] != 0) {
		loff_t pos = base->l1_table[l1_index];
		__be64 *disk_l2;
		ssize_t ret;
		int j;

		disk_l2 = kvmalloc(base->cluster_size, GFP_NOIO);
		if (!disk_l2) {
			best->valid = false;
			return NULL;
		}

		ret = kernel_read(base->file, disk_l2,
				  base->cluster_size, &pos);
		if (ret != base->cluster_size) {
			pr_warn("lbd: base L2 read failed for l1[%u]\n",
				l1_index);
			kvfree(disk_l2);
			best->valid = false;
			return NULL;
		}

		/* Verify CRC32C trailer */
		{
			u8 *raw = (u8 *)disk_l2;
			u32 stored_crc = _qcow2_get32(raw, base->cluster_size - 4);
			u32 calc_crc = ~crc32c(~0, raw, base->cluster_size - 4);

			if (stored_crc != calc_crc) {
				pr_warn("lbd: base L2 CRC32C mismatch for l1[%u]: "
					"stored=0x%08x computed=0x%08x\n",
					l1_index, stored_crc, calc_crc);
				kvfree(disk_l2);
				best->valid = false;
				return NULL;
			}
		}

		for (j = 0; j < base->l2_entries; j++)
			best->table[j] = be64_to_cpu(disk_l2[j]);

		kvfree(disk_l2);
	} else {
		/* Unallocated L2: all zeros */
		memset(best->table, 0, base->cluster_size);
	}

	best->valid = true;
	return best;
}

/*
 * Read a full cluster from the base layer into buf.
 * Returns 0 on success, negative errno on failure.
 */
static int lbd_qcow2_base_read_cluster(struct lbd_device *dev,
					u64 cluster_index, void *buf)
{
	struct lbd_qcow2_base *base = dev->base;
	u32 cluster_size = dev->qcow2.cluster_size;

	if (!base->is_qcow2) {
		/* Raw base: direct read */
		loff_t pos = cluster_index * cluster_size;
		ssize_t ret;

		if (pos + cluster_size > base->size) {
			/* Unallocated: signal with return 1 */
			return 1;
		}

		ret = kernel_read(base->file, buf, cluster_size, &pos);
		if (ret != cluster_size) {
			pr_warn("lbd%d: base raw read failed at cluster %llu\n",
				dev->index, cluster_index);
			return ret < 0 ? ret : -EIO;
		}
		return 0;
	}

	/* qcow2 base: L1/L2 lookup */
	{
		u32 l1_idx = cluster_index / base->l2_entries;
		u32 l2_idx = cluster_index % base->l2_entries;
		struct lbd_l2_cache_entry *l2e;
		u64 l2_entry, phys_offset;
		ssize_t ret;

		l2e = lbd_qcow2_base_l2_get(base, l1_idx);
		if (!l2e)
			return -EIO;

		l2_entry = l2e->table[l2_idx];

		if (l2_entry == 0) {
			/* Unallocated in base: signal with return 1 */
			return 1;
		}

		phys_offset = l2_entry & LBD_QCOW2_L2_OFFSET_MASK;

		if (l2_entry & LBD_QCOW2_L2_COMPRESSED) {
			__be32 comp_size_be;
			u32 comp_size;
			int dec_len;
			loff_t pos = phys_offset;

			ret = kernel_read(base->file, &comp_size_be,
					  sizeof(comp_size_be), &pos);
			if (ret != sizeof(comp_size_be)) {
				pr_warn("lbd%d: base compressed size read failed\n",
					dev->index);
				return ret < 0 ? ret : -EIO;
			}

			comp_size = be32_to_cpu(comp_size_be);
			if (comp_size > LZ4_compressBound(cluster_size)) {
				pr_warn("lbd%d: base invalid compressed size %u\n",
					dev->index, comp_size);
				return -EIO;
			}

			ret = kernel_read(base->file, base->read_buf,
					  comp_size, &pos);
			if (ret != comp_size) {
				pr_warn("lbd%d: base compressed data read failed\n",
					dev->index);
				return ret < 0 ? ret : -EIO;
			}

			dec_len = LZ4_decompress_safe(base->read_buf, buf,
						      comp_size, cluster_size);
			if (dec_len != cluster_size) {
				pr_warn("lbd%d: base LZ4 decompress failed (%d)\n",
					dev->index, dec_len);
				return -EIO;
			}
		} else {
			/* Uncompressed cluster */
			loff_t pos = phys_offset;

			ret = kernel_read(base->file, buf,
					  cluster_size, &pos);
			if (ret != cluster_size) {
				pr_warn("lbd%d: base cluster read failed\n",
					dev->index);
				return ret < 0 ? ret : -EIO;
			}
		}
	}

	return 0;
}

/* ----------------------------------------------------------------
 * Swap base layer and reload L1
 * ---------------------------------------------------------------- */

/*
 * Re-read the primary's L1 table from disk.
 * Called with rwsem held for write.
 */
static int lbd_qcow2_reload_l1(struct lbd_qcow2 *q, struct lbd_device *dev)
{
	u8 *hdr;
	loff_t pos = 0;
	ssize_t ret;
	__be64 *disk_l1;
	u64 *new_l1;
	u32 new_l1_size;
	loff_t new_l1_offset;
	int i;

	hdr = kvmalloc(LBD_QCOW2_HEADER_SIZE, GFP_NOIO);
	if (!hdr)
		return -ENOMEM;

	ret = kernel_read(dev->backing_file, hdr, LBD_QCOW2_HEADER_SIZE, &pos);
	if (ret != LBD_QCOW2_HEADER_SIZE) {
		kvfree(hdr);
		return ret < 0 ? ret : -EIO;
	}

	new_l1_offset = lbd_qcow2_hdr_l1_table_offset(hdr);
	new_l1_size = lbd_qcow2_hdr_l1_size(hdr);
	q->alloc_offset = lbd_qcow2_hdr_alloc_offset(hdr);
	q->free_list_head = lbd_qcow2_hdr_free_list(hdr);
	kvfree(hdr);

	new_l1 = kvmalloc_array(new_l1_size, sizeof(u64), GFP_NOIO);
	if (!new_l1)
		return -ENOMEM;

	disk_l1 = kvmalloc_array(new_l1_size, sizeof(__be64), GFP_NOIO);
	if (!disk_l1) {
		kvfree(new_l1);
		return -ENOMEM;
	}

	pos = new_l1_offset;
	ret = kernel_read(dev->backing_file, disk_l1,
			  new_l1_size * sizeof(__be64), &pos);
	if (ret != new_l1_size * sizeof(__be64)) {
		kvfree(disk_l1);
		kvfree(new_l1);
		return ret < 0 ? ret : -EIO;
	}

	for (i = 0; i < new_l1_size; i++)
		new_l1[i] = be64_to_cpu(disk_l1[i]);
	kvfree(disk_l1);

	kvfree(q->l1_table);
	q->l1_table = new_l1;
	q->l1_size = new_l1_size;
	q->l1_offset = new_l1_offset;

	return 0;
}

/*
 * Invalidate all caches (primary + base).
 * Called with rwsem held for write.
 */
static void lbd_qcow2_invalidate_all_caches(struct lbd_qcow2 *q,
					     struct lbd_qcow2_base *base)
{
	int i;

	/* Primary cluster cache */
	for (i = 0; i < LBD_QCOW2_CL_CACHE_SIZE; i++)
		q->cl_cache[i].valid = false;

	/* Primary L2 cache */
	for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++)
		q->l2_cache[i].valid = false;

	/* Base layer caches */
	if (base && base->is_qcow2) {
		for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++)
			base->l2_cache[i].valid = false;
	}
}

int lbd_qcow2_swap_base(struct lbd_device *dev, const char *new_path)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	int ret;

	down_write(&q->rwsem);

	/* Flush all dirty L2 tables */
	lbd_qcow2_flush_all_l2(dev);

	/* Close old base */
	if (dev->base) {
		lbd_qcow2_base_destroy(dev->base);
		dev->base = NULL;
	}

	/* Open new base */
	ret = lbd_qcow2_base_init(dev, new_path);
	if (ret) {
		pr_warn("lbd%d: swap_base failed to open '%s': %d\n",
			dev->index, new_path, ret);
		up_write(&q->rwsem);
		return ret;
	}

	/* Reload primary L1 table from disk */
	ret = lbd_qcow2_reload_l1(q, dev);
	if (ret) {
		pr_warn("lbd%d: swap_base failed to reload L1: %d\n",
			dev->index, ret);
		up_write(&q->rwsem);
		return ret;
	}

	/* Invalidate all cached data */
	lbd_qcow2_invalidate_all_caches(q, dev->base);

	/* Update stored path */
	strscpy(dev->base_path, new_path, sizeof(dev->base_path));

	up_write(&q->rwsem);

	pr_info("lbd%d: base layer swapped to '%s'\n", dev->index, new_path);
	return 0;
}

int lbd_qcow2_base_init(struct lbd_device *dev, const char *path)
{
	struct lbd_qcow2 *q = &dev->qcow2;
	struct lbd_qcow2_base *base;
	struct file *f;
	struct inode *inode, *primary_inode;
	u64 magic;
	loff_t pos;
	ssize_t ret;
	int i;

	f = filp_open(path, O_RDONLY | O_LARGEFILE, 0);
	if (IS_ERR(f)) {
		pr_err("lbd%d: cannot open base file '%s': %ld\n",
		       dev->index, path, PTR_ERR(f));
		return PTR_ERR(f);
	}

	inode = file_inode(f);
	if (!S_ISREG(inode->i_mode)) {
		pr_err("lbd%d: base path must be a regular file\n",
		       dev->index);
		fput(f);
		return -EINVAL;
	}

	if (i_size_read(inode) == 0) {
		pr_err("lbd%d: base file is empty\n", dev->index);
		fput(f);
		return -EINVAL;
	}

	/* Base and primary must be different files */
	primary_inode = file_inode(dev->backing_file);
	if (inode->i_sb == primary_inode->i_sb &&
	    inode->i_ino == primary_inode->i_ino) {
		pr_err("lbd%d: base and primary must be different files\n",
		       dev->index);
		fput(f);
		return -EINVAL;
	}

	base = kzalloc(sizeof(*base), GFP_KERNEL);
	if (!base) {
		fput(f);
		return -ENOMEM;
	}

	base->file = f;
	base->lru_tick = 0;

	/* Detect raw vs qcow2 via magic */
	pos = 0;
	ret = kernel_read(f, &magic, 8, &pos);
	if (ret != 8) {
		pr_err("lbd%d: cannot read base file header\n", dev->index);
		kfree(base);
		fput(f);
		return ret < 0 ? ret : -EIO;
	}

	if (be64_to_cpu(magic) == LBD_QCOW2_MAGIC) {
		/* qcow2 base */
		u8 *hdr;
		__be64 *disk_l1;

		base->is_qcow2 = true;

		hdr = kvmalloc(LBD_QCOW2_HEADER_SIZE, GFP_KERNEL);
		if (!hdr) {
			kfree(base);
			fput(f);
			return -ENOMEM;
		}

		pos = 0;
		ret = kernel_read(f, hdr, LBD_QCOW2_HEADER_SIZE, &pos);
		if (ret != LBD_QCOW2_HEADER_SIZE) {
			pr_err("lbd%d: base qcow2 header read failed\n",
			       dev->index);
			kvfree(hdr);
			kfree(base);
			fput(f);
			return ret < 0 ? ret : -EIO;
		}

		if (lbd_qcow2_hdr_version(hdr) != LBD_QCOW2_VERSION) {
			pr_err("lbd%d: base qcow2 version mismatch (%u)\n",
			       dev->index, lbd_qcow2_hdr_version(hdr));
			kvfree(hdr);
			kfree(base);
			fput(f);
			return -EINVAL;
		}

		base->cluster_bits = lbd_qcow2_hdr_cluster_bits(hdr);
		if (base->cluster_bits != q->cluster_bits) {
			pr_err("lbd%d: base cluster_bits %u != primary %u\n",
			       dev->index, base->cluster_bits, q->cluster_bits);
			kvfree(hdr);
			kfree(base);
			fput(f);
			return -EINVAL;
		}

		base->cluster_size = 1U << base->cluster_bits;
		base->l2_entries = (base->cluster_size - LBD_QCOW2_L2_TRAILER_SIZE) / sizeof(u64);
		base->size = lbd_qcow2_hdr_virtual_size(hdr);
		base->l1_offset = lbd_qcow2_hdr_l1_table_offset(hdr);
		base->l1_size = lbd_qcow2_hdr_l1_size(hdr);

		kvfree(hdr);

		/* Validate virtual size matches primary */
		if (base->size != q->virtual_size) {
			pr_err("lbd%d: base virtual_size %llu != primary %llu\n",
			       dev->index, base->size, q->virtual_size);
			kfree(base);
			fput(f);
			return -EINVAL;
		}

		/* Load L1 table */
		base->l1_table = kvmalloc_array(base->l1_size, sizeof(u64),
						GFP_KERNEL);
		if (!base->l1_table) {
			kfree(base);
			fput(f);
			return -ENOMEM;
		}

		disk_l1 = kvmalloc_array(base->l1_size, sizeof(__be64),
					 GFP_KERNEL);
		if (!disk_l1) {
			kvfree(base->l1_table);
			kfree(base);
			fput(f);
			return -ENOMEM;
		}

		pos = base->l1_offset;
		ret = kernel_read(f, disk_l1,
				  base->l1_size * sizeof(__be64), &pos);
		if (ret != base->l1_size * sizeof(__be64)) {
			pr_err("lbd%d: base L1 table read failed\n",
			       dev->index);
			kvfree(disk_l1);
			kvfree(base->l1_table);
			kfree(base);
			fput(f);
			return ret < 0 ? ret : -EIO;
		}

		for (i = 0; i < base->l1_size; i++)
			base->l1_table[i] = be64_to_cpu(disk_l1[i]);
		kvfree(disk_l1);

		/* Allocate L2 cache tables */
		for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++) {
			struct lbd_l2_cache_entry *e = &base->l2_cache[i];

			e->table = kvmalloc(base->cluster_size, GFP_KERNEL);
			if (!e->table)
				goto err_l2_cache;
			e->valid = false;
			e->dirty = false;
		}

		/* Allocate read buffer for decompression */
		base->read_buf = kvmalloc(LZ4_compressBound(base->cluster_size),
					  GFP_KERNEL);
		if (!base->read_buf)
			goto err_l2_cache;
	} else {
		/* Raw base */
		base->is_qcow2 = false;
		base->size = i_size_read(inode);

		/* Validate size matches primary */
		if (base->size != q->virtual_size) {
			pr_err("lbd%d: base size %llu != primary virtual_size %llu\n",
			       dev->index, base->size, q->virtual_size);
			kfree(base);
			fput(f);
			return -EINVAL;
		}
	}

	dev->base = base;
	pr_info("lbd%d: base layer attached (%s, %llu bytes)\n",
		dev->index, base->is_qcow2 ? "qcow2-lz4" : "raw",
		base->size);
	return 0;

err_l2_cache:
	if (base->is_qcow2) {
		kvfree(base->read_buf);
		for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++)
			kvfree(base->l2_cache[i].table);
		kvfree(base->l1_table);
	}
	kfree(base);
	fput(f);
	return -ENOMEM;
}

void lbd_qcow2_base_destroy(struct lbd_qcow2_base *base)
{
	int i;

	if (base->is_qcow2) {
		kvfree(base->read_buf);
		for (i = 0; i < LBD_QCOW2_L2_CACHE_SIZE; i++)
			kvfree(base->l2_cache[i].table);
		kvfree(base->l1_table);
	}

	fput(base->file);
	kfree(base);
}
