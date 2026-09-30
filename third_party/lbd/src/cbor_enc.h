/* SPDX-License-Identifier: GPL-2.0 */
#ifndef _CBOR_ENC_H
#define _CBOR_ENC_H

/*
 * Minimal CBOR encoder (RFC 8949) for kernel use.
 * Supports major types 0 (uint), 2 (byte string), 3 (text string), 5 (map).
 */

#include <linux/types.h>
#include <linux/errno.h>
#include <linux/string.h>

struct cbor_enc {
	u8 *buf;
	size_t pos;
	size_t cap;
	int err;	/* sticky -ENOSPC */
};

static inline void cbor_enc_init(struct cbor_enc *e, void *buf, size_t cap)
{
	e->buf = buf;
	e->pos = 0;
	e->cap = cap;
	e->err = 0;
}

static inline size_t cbor_enc_len(const struct cbor_enc *e)
{
	return e->pos;
}

/* Write one byte, set sticky error on overflow */
static inline void cbor_put(struct cbor_enc *e, u8 b)
{
	if (e->err)
		return;
	if (e->pos >= e->cap) {
		e->err = -ENOSPC;
		return;
	}
	e->buf[e->pos++] = b;
}

/*
 * Encode a CBOR head: major type (top 3 bits) + value.
 * Chooses the shortest encoding automatically.
 */
static inline void cbor_enc_head(struct cbor_enc *e, u8 major, u64 val)
{
	u8 mt = major << 5;

	if (val < 24) {
		cbor_put(e, mt | (u8)val);
	} else if (val <= 0xFF) {
		cbor_put(e, mt | 24);
		cbor_put(e, (u8)val);
	} else if (val <= 0xFFFF) {
		cbor_put(e, mt | 25);
		cbor_put(e, (u8)(val >> 8));
		cbor_put(e, (u8)val);
	} else if (val <= 0xFFFFFFFF) {
		cbor_put(e, mt | 26);
		cbor_put(e, (u8)(val >> 24));
		cbor_put(e, (u8)(val >> 16));
		cbor_put(e, (u8)(val >> 8));
		cbor_put(e, (u8)val);
	} else {
		cbor_put(e, mt | 27);
		cbor_put(e, (u8)(val >> 56));
		cbor_put(e, (u8)(val >> 48));
		cbor_put(e, (u8)(val >> 40));
		cbor_put(e, (u8)(val >> 32));
		cbor_put(e, (u8)(val >> 24));
		cbor_put(e, (u8)(val >> 16));
		cbor_put(e, (u8)(val >> 8));
		cbor_put(e, (u8)val);
	}
}

/* Major type 5: map of count pairs */
static inline void cbor_enc_map(struct cbor_enc *e, u64 count)
{
	cbor_enc_head(e, 5, count);
}

/* Major type 0: unsigned integer */
static inline void cbor_enc_uint(struct cbor_enc *e, u64 val)
{
	cbor_enc_head(e, 0, val);
}

/* Major type 3: text string (UTF-8) */
static inline void cbor_enc_text(struct cbor_enc *e, const char *s, size_t len)
{
	size_t i;

	cbor_enc_head(e, 3, len);
	for (i = 0; i < len; i++)
		cbor_put(e, (u8)s[i]);
}

/* Convenience: encode a text string key (NUL-terminated) */
static inline void cbor_enc_text_key(struct cbor_enc *e, const char *key)
{
	cbor_enc_text(e, key, strlen(key));
}

/* Major type 2: byte string header only (caller appends raw data) */
static inline void cbor_enc_bytes_hdr(struct cbor_enc *e, u64 len)
{
	cbor_enc_head(e, 2, len);
}

#endif /* _CBOR_ENC_H */
