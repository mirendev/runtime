/* SPDX-License-Identifier: GPL-2.0 */
#ifndef _LBD_QCOW2_H
#define _LBD_QCOW2_H

#include <linux/types.h>
#include <linux/rwsem.h>

#include "lbd_qcow2_format.h"

/* Cache sizes */
#define LBD_QCOW2_L2_CACHE_SIZE	16
#define LBD_QCOW2_CL_CACHE_SIZE	16
/* ----------------------------------------------------------------
 * In-memory cache structures
 * ---------------------------------------------------------------- */

struct lbd_l2_cache_entry {
	u32  l1_index;
	bool valid;
	bool dirty;
	u64  lru;
	u64  *table;  /* host-endian L2 entries, kvmalloc(cluster_size) */
};

struct lbd_cl_cache_entry {
	u64  cluster_index;
	bool valid;
	bool dirty;
	u64  lru;
	void *data;   /* kvmalloc(cluster_size) */
};

/* ----------------------------------------------------------------
 * Read-only base layer state (for thin snapshots)
 * ---------------------------------------------------------------- */

struct lbd_qcow2_base {
	struct file *file;
	bool        is_qcow2;
	u64         size;		/* raw: file size, qcow2: virtual_size */

	/* Only valid when is_qcow2 == true */
	u32         cluster_bits;
	u32         cluster_size;
	u32         l2_entries;
	u64         *l1_table;
	u32         l1_size;
	loff_t      l1_offset;

	struct lbd_l2_cache_entry l2_cache[LBD_QCOW2_L2_CACHE_SIZE];
	u64         lru_tick;

	void        *read_buf;		/* decompression buffer */
};

/* ----------------------------------------------------------------
 * Per-device qcow2 state (embedded in struct lbd_device)
 * ---------------------------------------------------------------- */

struct lbd_qcow2 {
	u32     cluster_bits;
	u32     cluster_size;		/* 1 << cluster_bits */
	u32     l2_entries;		/* (cluster_size - L2_TRAILER_SIZE) / 8 */
	u64     virtual_size;

	u64     *l1_table;		/* host-endian, always resident */
	u32     l1_size;
	loff_t  l1_offset;

	loff_t  alloc_offset;		/* append-only allocation cursor */
	loff_t  free_list_head;		/* head of on-disk free list, 0 if empty */

	struct lbd_l2_cache_entry  l2_cache[LBD_QCOW2_L2_CACHE_SIZE];
	struct lbd_cl_cache_entry  cl_cache[LBD_QCOW2_CL_CACHE_SIZE];
	u64     lru_tick;		/* monotonic counter for LRU */

	struct rw_semaphore rwsem;	/* read-shared, write-exclusive */

	void    *comp_buf;		/* LZ4_compressBound(cluster_size) */
	void    *read_buf;		/* cluster_size, for reading compressed */
};

/* ----------------------------------------------------------------
 * Function declarations
 * ---------------------------------------------------------------- */

struct lbd_device;
struct request;

int  lbd_qcow2_init(struct lbd_device *dev);
void lbd_qcow2_destroy(struct lbd_device *dev);
int  lbd_qcow2_read(struct lbd_device *dev, struct request *rq);
int  lbd_qcow2_write(struct lbd_device *dev, struct request *rq);
int  lbd_qcow2_discard(struct lbd_device *dev, struct request *rq);

/* Base layer (thin snapshot) */
int  lbd_qcow2_base_init(struct lbd_device *dev, const char *path);
void lbd_qcow2_base_destroy(struct lbd_qcow2_base *base);
int  lbd_qcow2_swap_base(struct lbd_device *dev, const char *new_path);

#endif /* _LBD_QCOW2_H */
