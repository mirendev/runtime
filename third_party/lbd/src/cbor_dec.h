/* SPDX-License-Identifier: GPL-2.0 */
#ifndef _CBOR_DEC_H
#define _CBOR_DEC_H

/*
 * Minimal CBOR decoder (RFC 8949) for kernel use.
 * Buffer-based companion to cbor_enc.h.
 * Supports major types 0 (uint), 3 (text string), 5 (map).
 */

#include <linux/types.h>
#include <linux/errno.h>
#include <linux/string.h>

struct cbor_dec {
	const u8 *buf;
	size_t pos;
	size_t len;
};

static inline void cbor_dec_init(struct cbor_dec *d, const void *buf,
				 size_t len)
{
	d->buf = buf;
	d->pos = 0;
	d->len = len;
}

/*
 * Read a CBOR head: major type (0-7) and argument value.
 * Returns 0 on success, -EINVAL on truncation or reserved additional info.
 */
static inline int cbor_dec_head(struct cbor_dec *d, u8 *major, u64 *val)
{
	u8 ib, ai;

	if (d->pos >= d->len)
		return -EINVAL;

	ib = d->buf[d->pos++];
	*major = ib >> 5;
	ai = ib & 0x1F;

	if (ai < 24) {
		*val = ai;
	} else if (ai == 24) {
		if (d->pos + 1 > d->len)
			return -EINVAL;
		*val = d->buf[d->pos++];
	} else if (ai == 25) {
		if (d->pos + 2 > d->len)
			return -EINVAL;
		*val = ((u64)d->buf[d->pos] << 8) | d->buf[d->pos + 1];
		d->pos += 2;
	} else if (ai == 26) {
		if (d->pos + 4 > d->len)
			return -EINVAL;
		*val = ((u64)d->buf[d->pos] << 24) |
		       ((u64)d->buf[d->pos + 1] << 16) |
		       ((u64)d->buf[d->pos + 2] << 8) |
		       d->buf[d->pos + 3];
		d->pos += 4;
	} else if (ai == 27) {
		if (d->pos + 8 > d->len)
			return -EINVAL;
		*val = ((u64)d->buf[d->pos] << 56) |
		       ((u64)d->buf[d->pos + 1] << 48) |
		       ((u64)d->buf[d->pos + 2] << 40) |
		       ((u64)d->buf[d->pos + 3] << 32) |
		       ((u64)d->buf[d->pos + 4] << 24) |
		       ((u64)d->buf[d->pos + 5] << 16) |
		       ((u64)d->buf[d->pos + 6] << 8) |
		       d->buf[d->pos + 7];
		d->pos += 8;
	} else {
		return -EINVAL; /* indefinite / reserved */
	}

	return 0;
}

/* Expect a map header (major 5), returns item count via *count */
static inline int cbor_dec_map(struct cbor_dec *d, u64 *count)
{
	u8 major;
	int ret = cbor_dec_head(d, &major, count);

	if (ret)
		return ret;
	if (major != 5)
		return -EINVAL;
	return 0;
}

/* Expect an unsigned integer (major 0) */
static inline int cbor_dec_uint(struct cbor_dec *d, u64 *val)
{
	u8 major;
	int ret = cbor_dec_head(d, &major, val);

	if (ret)
		return ret;
	if (major != 0)
		return -EINVAL;
	return 0;
}

/* Read a text string (major 3) into buf, NUL-terminated */
static inline int cbor_dec_text(struct cbor_dec *d, char *buf, size_t cap,
				size_t *outlen)
{
	u8 major;
	u64 slen;
	int ret = cbor_dec_head(d, &major, &slen);

	if (ret)
		return ret;
	if (major != 3)
		return -EINVAL;
	if (slen >= cap)
		return -EINVAL;
	if (d->pos + slen > d->len)
		return -EINVAL;

	memcpy(buf, d->buf + d->pos, slen);
	buf[slen] = '\0';
	d->pos += slen;
	if (outlen)
		*outlen = slen;
	return 0;
}

#endif /* _CBOR_DEC_H */
