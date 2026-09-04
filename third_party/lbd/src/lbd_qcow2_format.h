/* SPDX-License-Identifier: GPL-2.0 */
/*
 * LBD qcow2-lz4 on-disk format definitions.
 *
 * Shared between kernel module and userspace tools (lbdctl).
 * All header fields are big-endian on disk and accessed at explicit
 * byte offsets — no struct casting.
 */
#ifndef _LBD_QCOW2_FORMAT_H
#define _LBD_QCOW2_FORMAT_H

/* ----------------------------------------------------------------
 * Constants
 * ---------------------------------------------------------------- */

#define LBD_QCOW2_MAGIC		0x4C42444351573200ULL  /* "LBDQCW2\0" */
#define LBD_QCOW2_VERSION		2
#define LBD_QCOW2_COMP_LZ4		1
#define LBD_QCOW2_HEADER_SIZE		4096
#define LBD_QCOW2_L2_TRAILER_SIZE	8	/* 4 bytes reserved + 4 bytes CRC32C */

/* Header field byte offsets */
#define LBD_QCOW2_OFF_MAGIC		0	/* u64 */
#define LBD_QCOW2_OFF_VERSION		8	/* u32 */
#define LBD_QCOW2_OFF_CLUSTER_BITS	12	/* u32 */
#define LBD_QCOW2_OFF_VIRTUAL_SIZE	16	/* u64 */
#define LBD_QCOW2_OFF_L1_TABLE_OFFSET	24	/* u64 */
#define LBD_QCOW2_OFF_L1_SIZE		32	/* u32 */
#define LBD_QCOW2_OFF_ALLOC_OFFSET	36	/* u64 */
#define LBD_QCOW2_OFF_COMP_TYPE	44	/* u32 */
#define LBD_QCOW2_OFF_FREE_LIST	48	/* u64 */

/* Free list tombstone marker */
#define LBD_QCOW2_FREE_TOMBSTONE	0xDEADF4EEU

/* L2 entry flags */
#define LBD_QCOW2_L2_COMPRESSED	(1ULL << 63)
#define LBD_QCOW2_L2_OFFSET_MASK	0x3FFFFFFFFFFFFFFFULL

/* Default cluster parameters */
#define LBD_QCOW2_CLUSTER_BITS_DEFAULT	16  /* 64 KiB clusters */
#define LBD_QCOW2_CLUSTER_SIZE_DEFAULT	(1U << LBD_QCOW2_CLUSTER_BITS_DEFAULT)

/* ----------------------------------------------------------------
 * Byte-order helpers (kernel vs userspace)
 * ---------------------------------------------------------------- */

#ifdef __KERNEL__

#include <linux/types.h>
#include <linux/string.h>
#include <asm/byteorder.h>

typedef u8  _qcow2_u8;
typedef u32 _qcow2_u32;
typedef u64 _qcow2_u64;

static inline _qcow2_u64 _qcow2_get64(const void *buf, int off)
{
	__be64 v;
	memcpy(&v, (const u8 *)buf + off, 8);
	return be64_to_cpu(v);
}

static inline _qcow2_u32 _qcow2_get32(const void *buf, int off)
{
	__be32 v;
	memcpy(&v, (const u8 *)buf + off, 4);
	return be32_to_cpu(v);
}

static inline void _qcow2_put64(void *buf, int off, _qcow2_u64 val)
{
	__be64 v = cpu_to_be64(val);
	memcpy((u8 *)buf + off, &v, 8);
}

static inline void _qcow2_put32(void *buf, int off, _qcow2_u32 val)
{
	__be32 v = cpu_to_be32(val);
	memcpy((u8 *)buf + off, &v, 4);
}

typedef u16 _qcow2_u16;

static inline _qcow2_u16 _qcow2_get16(const void *buf, int off)
{
	__be16 v;
	memcpy(&v, (const u8 *)buf + off, 2);
	return be16_to_cpu(v);
}

static inline void _qcow2_put16(void *buf, int off, _qcow2_u16 val)
{
	__be16 v = cpu_to_be16(val);
	memcpy((u8 *)buf + off, &v, 2);
}

#else /* userspace */

#include <stdint.h>
#include <string.h>

typedef uint8_t  _qcow2_u8;
typedef uint32_t _qcow2_u32;
typedef uint64_t _qcow2_u64;

static inline _qcow2_u64 _qcow2_get64(const void *buf, int off)
{
	const uint8_t *p = (const uint8_t *)buf + off;
	return ((uint64_t)p[0] << 56) | ((uint64_t)p[1] << 48) |
	       ((uint64_t)p[2] << 40) | ((uint64_t)p[3] << 32) |
	       ((uint64_t)p[4] << 24) | ((uint64_t)p[5] << 16) |
	       ((uint64_t)p[6] <<  8) |  (uint64_t)p[7];
}

static inline _qcow2_u32 _qcow2_get32(const void *buf, int off)
{
	const uint8_t *p = (const uint8_t *)buf + off;
	return ((uint32_t)p[0] << 24) | ((uint32_t)p[1] << 16) |
	       ((uint32_t)p[2] <<  8) |  (uint32_t)p[3];
}

static inline void _qcow2_put64(void *buf, int off, _qcow2_u64 val)
{
	uint8_t *p = (uint8_t *)buf + off;
	p[0] = (uint8_t)(val >> 56); p[1] = (uint8_t)(val >> 48);
	p[2] = (uint8_t)(val >> 40); p[3] = (uint8_t)(val >> 32);
	p[4] = (uint8_t)(val >> 24); p[5] = (uint8_t)(val >> 16);
	p[6] = (uint8_t)(val >>  8); p[7] = (uint8_t)val;
}

static inline void _qcow2_put32(void *buf, int off, _qcow2_u32 val)
{
	uint8_t *p = (uint8_t *)buf + off;
	p[0] = (uint8_t)(val >> 24); p[1] = (uint8_t)(val >> 16);
	p[2] = (uint8_t)(val >>  8); p[3] = (uint8_t)val;
}

typedef uint16_t _qcow2_u16;

static inline _qcow2_u16 _qcow2_get16(const void *buf, int off)
{
	const uint8_t *p = (const uint8_t *)buf + off;
	return ((uint16_t)p[0] << 8) | (uint16_t)p[1];
}

static inline void _qcow2_put16(void *buf, int off, _qcow2_u16 val)
{
	uint8_t *p = (uint8_t *)buf + off;
	p[0] = (uint8_t)(val >> 8); p[1] = (uint8_t)val;
}

#endif /* __KERNEL__ */

/* ----------------------------------------------------------------
 * Per-field accessors: getters
 * ---------------------------------------------------------------- */

static inline _qcow2_u64 lbd_qcow2_hdr_magic(const void *h)
{
	return _qcow2_get64(h, LBD_QCOW2_OFF_MAGIC);
}

static inline _qcow2_u32 lbd_qcow2_hdr_version(const void *h)
{
	return _qcow2_get32(h, LBD_QCOW2_OFF_VERSION);
}

static inline _qcow2_u32 lbd_qcow2_hdr_cluster_bits(const void *h)
{
	return _qcow2_get32(h, LBD_QCOW2_OFF_CLUSTER_BITS);
}

static inline _qcow2_u64 lbd_qcow2_hdr_virtual_size(const void *h)
{
	return _qcow2_get64(h, LBD_QCOW2_OFF_VIRTUAL_SIZE);
}

static inline _qcow2_u64 lbd_qcow2_hdr_l1_table_offset(const void *h)
{
	return _qcow2_get64(h, LBD_QCOW2_OFF_L1_TABLE_OFFSET);
}

static inline _qcow2_u32 lbd_qcow2_hdr_l1_size(const void *h)
{
	return _qcow2_get32(h, LBD_QCOW2_OFF_L1_SIZE);
}

static inline _qcow2_u64 lbd_qcow2_hdr_alloc_offset(const void *h)
{
	return _qcow2_get64(h, LBD_QCOW2_OFF_ALLOC_OFFSET);
}

static inline _qcow2_u32 lbd_qcow2_hdr_comp_type(const void *h)
{
	return _qcow2_get32(h, LBD_QCOW2_OFF_COMP_TYPE);
}

static inline _qcow2_u64 lbd_qcow2_hdr_free_list(const void *h)
{
	return _qcow2_get64(h, LBD_QCOW2_OFF_FREE_LIST);
}

/* ----------------------------------------------------------------
 * Per-field accessors: setters
 * ---------------------------------------------------------------- */

static inline void lbd_qcow2_hdr_set_magic(void *h, _qcow2_u64 v)
{
	_qcow2_put64(h, LBD_QCOW2_OFF_MAGIC, v);
}

static inline void lbd_qcow2_hdr_set_version(void *h, _qcow2_u32 v)
{
	_qcow2_put32(h, LBD_QCOW2_OFF_VERSION, v);
}

static inline void lbd_qcow2_hdr_set_cluster_bits(void *h, _qcow2_u32 v)
{
	_qcow2_put32(h, LBD_QCOW2_OFF_CLUSTER_BITS, v);
}

static inline void lbd_qcow2_hdr_set_virtual_size(void *h, _qcow2_u64 v)
{
	_qcow2_put64(h, LBD_QCOW2_OFF_VIRTUAL_SIZE, v);
}

static inline void lbd_qcow2_hdr_set_l1_table_offset(void *h, _qcow2_u64 v)
{
	_qcow2_put64(h, LBD_QCOW2_OFF_L1_TABLE_OFFSET, v);
}

static inline void lbd_qcow2_hdr_set_l1_size(void *h, _qcow2_u32 v)
{
	_qcow2_put32(h, LBD_QCOW2_OFF_L1_SIZE, v);
}

static inline void lbd_qcow2_hdr_set_alloc_offset(void *h, _qcow2_u64 v)
{
	_qcow2_put64(h, LBD_QCOW2_OFF_ALLOC_OFFSET, v);
}

static inline void lbd_qcow2_hdr_set_comp_type(void *h, _qcow2_u32 v)
{
	_qcow2_put32(h, LBD_QCOW2_OFF_COMP_TYPE, v);
}

static inline void lbd_qcow2_hdr_set_free_list(void *h, _qcow2_u64 v)
{
	_qcow2_put64(h, LBD_QCOW2_OFF_FREE_LIST, v);
}

#endif /* _LBD_QCOW2_FORMAT_H */
