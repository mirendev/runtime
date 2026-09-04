// SPDX-License-Identifier: GPL-2.0
/*
 * lbdctl - userspace control tool for LBD (Logging Block Device)
 *
 * Usage:
 *   lbdctl add [--json] --log-dir /path/to/logs /path/to/file.img
 *   lbdctl remove [--json] N           - destroy /dev/lbdN
 *   lbdctl list [--json]               - show all active devices
 *   lbdctl log [--json] /path/to/file.img.log  - dump log
 */

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <poll.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>
#include <linux/types.h>
#include "lz4/lz4.h"

/* Mirror the kernel ioctl structures from lbd.h */
#define LBD_LOG_PATH_MAX	256

/* CBOR log numeric map keys */
#define LBD_CBOR_KEY_HDR_VERSION	1
#define LBD_CBOR_KEY_HDR_BLOCK_SIZE	2
#define LBD_CBOR_KEY_HDR_SEGMENT_LABEL	3
#define LBD_CBOR_KEY_HDR_DEVICE_SIZE	4
#define LBD_CBOR_KEY_HDR_BACKING_PATH	5

#define LBD_CBOR_KEY_OP			1
#define LBD_CBOR_KEY_TIMESTAMP		2
#define LBD_CBOR_KEY_SEQUENCE		3
#define LBD_CBOR_KEY_BLOCK		4
#define LBD_CBOR_KEY_LENGTH		5
#define LBD_CBOR_KEY_CHECKSUM		6
#define LBD_CBOR_KEY_DATA		7

/* Watch command keys (write path) */
#define LBD_WATCH_KEY_CMD		1
#define LBD_WATCH_KEY_DEV		2
#define LBD_WATCH_KEY_PATH		3

/* Miss event keys */
#define LBD_MISS_KEY_TYPE		1
#define LBD_MISS_KEY_DEV		2
#define LBD_MISS_KEY_CLUSTER		3

/* Event keys (read path) */
#define LBD_EVENT_KEY_TYPE		1
#define LBD_EVENT_KEY_DEV		2
#define LBD_EVENT_KEY_LABEL		3
#define LBD_EVENT_KEY_DIR		4
#define LBD_EVENT_KEY_SEQ		5
#define LBD_EVENT_KEY_SIZE		6

#define LBD_CTL_MAGIC		'L'

struct lbd_ctl_add {
	char path[LBD_LOG_PATH_MAX];
	char log_dir[LBD_LOG_PATH_MAX];
	char base_path[LBD_LOG_PATH_MAX];  /* empty string = no base */
	__s32 index;
	__u64 log_max_size;
	__u32 log_max_age_secs;
};

struct lbd_ctl_remove {
	__s32 index;
};

struct lbd_ctl_info {
	__s32 index;
	__u32 state;
	__u64 size;
	char path[LBD_LOG_PATH_MAX];
};

#define LBD_CTL_ADD		_IOWR(LBD_CTL_MAGIC, 0, struct lbd_ctl_add)
#define LBD_CTL_REMOVE		_IOW(LBD_CTL_MAGIC, 1, struct lbd_ctl_remove)
#define LBD_CTL_INFO		_IOWR(LBD_CTL_MAGIC, 2, struct lbd_ctl_info)

#define LBD_CTL_PATH		"/dev/lbd-control"

/* ----------------------------------------------------------------
 * CRC32 (IEEE 802.3 polynomial, matches Linux kernel crc32())
 * ---------------------------------------------------------------- */

static uint32_t crc32_table[256];
static int crc32_table_ready;

static void crc32_init(void)
{
	uint32_t poly = 0xEDB88320;
	for (int i = 0; i < 256; i++) {
		uint32_t c = i;
		for (int j = 0; j < 8; j++)
			c = (c >> 1) ^ (poly & (-(c & 1)));
		crc32_table[i] = c;
	}
	crc32_table_ready = 1;
}

static uint32_t crc32_calc(const void *buf, size_t len)
{
	if (!crc32_table_ready)
		crc32_init();
	const uint8_t *p = buf;
	uint32_t crc = 0xFFFFFFFF;
	for (size_t i = 0; i < len; i++)
		crc = crc32_table[(crc ^ p[i]) & 0xFF] ^ (crc >> 8);
	return crc ^ 0xFFFFFFFF;
}

/* ----------------------------------------------------------------
 * CRC32C (Castagnoli polynomial 0x82F63B78, matches Linux crc32c())
 * ---------------------------------------------------------------- */

static uint32_t crc32c_table[256];
static int crc32c_table_ready;

static void crc32c_init(void)
{
	uint32_t poly = 0x82F63B78;
	for (int i = 0; i < 256; i++) {
		uint32_t c = i;
		for (int j = 0; j < 8; j++)
			c = (c >> 1) ^ (poly & (-(c & 1)));
		crc32c_table[i] = c;
	}
	crc32c_table_ready = 1;
}

static uint32_t crc32c_calc(const void *buf, size_t len)
{
	if (!crc32c_table_ready)
		crc32c_init();
	const uint8_t *p = buf;
	uint32_t crc = 0xFFFFFFFF;
	for (size_t i = 0; i < len; i++)
		crc = crc32c_table[(crc ^ p[i]) & 0xFF] ^ (crc >> 8);
	return crc ^ 0xFFFFFFFF;
}

/* ----------------------------------------------------------------
 * Helpers
 * ---------------------------------------------------------------- */

static const char *state_str(unsigned int state)
{
	switch (state) {
	case 0: return "unbound";
	case 1: return "bound";
	case 2: return "removing";
	default: return "unknown";
	}
}

static int open_ctl(void)
{
	int fd = open(LBD_CTL_PATH, O_RDWR);
	if (fd < 0) {
		fprintf(stderr, "Cannot open %s: %s\n",
			LBD_CTL_PATH, strerror(errno));
		if (errno == ENOENT)
			fprintf(stderr, "Is the lbd module loaded?\n");
	}
	return fd;
}

static int read_exact(int fd, void *buf, size_t len)
{
	size_t done = 0;
	while (done < len) {
		ssize_t n = read(fd, (char *)buf + done, len - done);
		if (n < 0) {
			if (errno == EINTR)
				continue;
			return -1;
		}
		if (n == 0)
			return done > 0 ? -1 : 0; /* EOF */
		done += n;
	}
	return 1; /* success */
}

/* Escape a string for JSON output (handles \, ", and control chars) */
static void json_print_string(FILE *out, const char *s)
{
	fputc('"', out);
	for (; *s; s++) {
		unsigned char c = *s;
		switch (c) {
		case '"':  fputs("\\\"", out); break;
		case '\\': fputs("\\\\", out); break;
		case '\b': fputs("\\b", out);  break;
		case '\f': fputs("\\f", out);  break;
		case '\n': fputs("\\n", out);  break;
		case '\r': fputs("\\r", out);  break;
		case '\t': fputs("\\t", out);  break;
		default:
			if (c < 0x20)
				fprintf(out, "\\u%04x", c);
			else
				fputc(c, out);
		}
	}
	fputc('"', out);
}

/* Format size with human-readable units */
static const char *fmt_size(uint64_t bytes, char *buf, size_t bufsz)
{
	if (bytes >= (1ULL << 30))
		snprintf(buf, bufsz, "%.1f GiB", (double)bytes / (1ULL << 30));
	else if (bytes >= (1ULL << 20))
		snprintf(buf, bufsz, "%.1f MiB", (double)bytes / (1ULL << 20));
	else if (bytes >= (1ULL << 10))
		snprintf(buf, bufsz, "%.1f KiB", (double)bytes / (1ULL << 10));
	else
		snprintf(buf, bufsz, "%llu B", (unsigned long long)bytes);
	return buf;
}

/* Print hex dump of data (16 bytes per line) */
static void hex_dump(FILE *out, const uint8_t *data, size_t len,
		     const char *indent)
{
	for (size_t off = 0; off < len; off += 16) {
		fprintf(out, "%s%08zx  ", indent, off);
		/* hex */
		for (size_t i = 0; i < 16; i++) {
			if (off + i < len)
				fprintf(out, "%02x ", data[off + i]);
			else
				fputs("   ", out);
			if (i == 7)
				fputc(' ', out);
		}
		fputs(" |", out);
		/* ascii */
		for (size_t i = 0; i < 16 && off + i < len; i++) {
			uint8_t c = data[off + i];
			fputc((c >= 0x20 && c < 0x7f) ? c : '.', out);
		}
		fputs("|\n", out);
	}
}

/* ----------------------------------------------------------------
 * Device control commands
 * ---------------------------------------------------------------- */

static int cmd_add(int argc, char **argv)
{
	struct lbd_ctl_add arg;
	char resolved[PATH_MAX];
	const char *path = NULL;
	const char *log_dir = NULL;
	const char *base = NULL;
	int fd, ret;
	int json = 0;

	memset(&arg, 0, sizeof(arg));

	for (int i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--json") == 0) {
			json = 1;
		} else if (strcmp(argv[i], "--log-max-size") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--log-max-size requires a value\n");
				return 1;
			}
			arg.log_max_size = strtoull(argv[i], NULL, 0);
		} else if (strcmp(argv[i], "--log-max-age") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--log-max-age requires a value\n");
				return 1;
			}
			arg.log_max_age_secs = strtoul(argv[i], NULL, 0);
		} else if (strcmp(argv[i], "--log-dir") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--log-dir requires a value\n");
				return 1;
			}
			log_dir = argv[i];
		} else if (strcmp(argv[i], "--base") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--base requires a value\n");
				return 1;
			}
			base = argv[i];
		} else if (!path) {
			path = argv[i];
		} else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	if (!path) {
		fprintf(stderr, "add requires a path argument\n");
		return 1;
	}

	if (!log_dir) {
		fprintf(stderr, "add requires --log-dir <directory>\n");
		return 1;
	}

	if (!realpath(path, resolved)) {
		fprintf(stderr, "Cannot resolve path '%s': %s\n",
			path, strerror(errno));
		return 1;
	}

	if (strlen(resolved) >= LBD_LOG_PATH_MAX) {
		fprintf(stderr, "Path too long (max %d)\n", LBD_LOG_PATH_MAX - 1);
		return 1;
	}

	snprintf(arg.path, LBD_LOG_PATH_MAX, "%s", resolved);

	if (!realpath(log_dir, resolved)) {
		fprintf(stderr, "Cannot resolve log directory '%s': %s\n",
			log_dir, strerror(errno));
		return 1;
	}

	if (strlen(resolved) >= LBD_LOG_PATH_MAX) {
		fprintf(stderr, "Log directory path too long (max %d)\n",
			LBD_LOG_PATH_MAX - 1);
		return 1;
	}

	snprintf(arg.log_dir, LBD_LOG_PATH_MAX, "%s", resolved);

	if (base) {
		if (!realpath(base, resolved)) {
			fprintf(stderr, "Cannot resolve base path '%s': %s\n",
				base, strerror(errno));
			return 1;
		}

		if (strlen(resolved) >= LBD_LOG_PATH_MAX) {
			fprintf(stderr, "Base path too long (max %d)\n",
				LBD_LOG_PATH_MAX - 1);
			return 1;
		}

		snprintf(arg.base_path, LBD_LOG_PATH_MAX, "%s", resolved);
	}

	fd = open_ctl();
	if (fd < 0)
		return 1;

	ret = ioctl(fd, LBD_CTL_ADD, &arg);
	if (ret < 0) {
		fprintf(stderr, "LBD_CTL_ADD failed: %s\n", strerror(errno));
		close(fd);
		return 1;
	}

	if (json) {
		printf("{\"device\": \"/dev/lbd%d\", \"index\": %d}\n",
		       arg.index, arg.index);
	} else {
		printf("Created /dev/lbd%d\n", arg.index);
	}
	close(fd);
	return 0;
}

static int cmd_remove(int argc, char **argv)
{
	struct lbd_ctl_remove arg;
	int fd, ret;
	int json = 0;
	const char *index_str = NULL;

	for (int i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--json") == 0)
			json = 1;
		else if (!index_str)
			index_str = argv[i];
		else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	if (!index_str) {
		fprintf(stderr, "remove requires an index argument\n");
		return 1;
	}

	arg.index = atoi(index_str);

	fd = open_ctl();
	if (fd < 0)
		return 1;

	ret = ioctl(fd, LBD_CTL_REMOVE, &arg);
	if (ret < 0) {
		fprintf(stderr, "LBD_CTL_REMOVE failed: %s\n", strerror(errno));
		close(fd);
		return 1;
	}

	if (json) {
		printf("{\"device\": \"/dev/lbd%d\", \"index\": %d}\n",
		       arg.index, arg.index);
	} else {
		printf("Removed /dev/lbd%d\n", arg.index);
	}
	close(fd);
	return 0;
}

static int cmd_list(int argc, char **argv)
{
	struct lbd_ctl_info info;
	int fd, i, found = 0;
	int json = 0;

	for (i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--json") == 0)
			json = 1;
		else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	fd = open_ctl();
	if (fd < 0)
		return 1;

	if (json) {
		printf("[");
	} else {
		printf("%-8s %-10s %-12s %s\n", "DEVICE", "STATE", "SIZE", "BACKING");
		printf("%-8s %-10s %-12s %s\n", "------", "-----", "----", "-------");
	}

	for (i = 0; i < 256; i++) {
		memset(&info, 0, sizeof(info));
		info.index = i;

		if (ioctl(fd, LBD_CTL_INFO, &info) < 0)
			continue;

		if (json) {
			if (found > 0)
				printf(",");
			printf("\n  {\"device\": \"/dev/lbd%d\", \"index\": %d, \"state\": \"%s\", \"size\": %llu, \"backing\": ",
			       info.index, info.index, state_str(info.state),
			       (unsigned long long)info.size);
			json_print_string(stdout, info.path);
			printf("}");
		} else {
			printf("lbd%-5d %-10s %-12llu %s\n",
			       info.index, state_str(info.state),
			       (unsigned long long)info.size, info.path);
		}
		found++;
	}

	if (json) {
		printf("\n]\n");
	} else if (!found) {
		printf("(no devices)\n");
	}

	close(fd);
	return 0;
}

/* ----------------------------------------------------------------
 * CBOR decoder (streaming, reads from fd)
 * ---------------------------------------------------------------- */

/*
 * Read a CBOR head: returns major type (0-7) and argument value.
 * Returns 0 on success, -1 on EOF, -2 on error.
 */
static int cbor_read_head(int fd, uint8_t *major_out, uint64_t *val_out)
{
	uint8_t ib;
	int ret = read_exact(fd, &ib, 1);
	if (ret == 0)
		return -1; /* EOF */
	if (ret < 0)
		return -2;

	*major_out = ib >> 5;
	uint8_t ai = ib & 0x1F;

	if (ai < 24) {
		*val_out = ai;
	} else if (ai == 24) {
		uint8_t b;
		if (read_exact(fd, &b, 1) <= 0) return -2;
		*val_out = b;
	} else if (ai == 25) {
		uint8_t b[2];
		if (read_exact(fd, b, 2) <= 0) return -2;
		*val_out = ((uint64_t)b[0] << 8) | b[1];
	} else if (ai == 26) {
		uint8_t b[4];
		if (read_exact(fd, b, 4) <= 0) return -2;
		*val_out = ((uint64_t)b[0] << 24) | ((uint64_t)b[1] << 16) |
			   ((uint64_t)b[2] << 8) | b[3];
	} else if (ai == 27) {
		uint8_t b[8];
		if (read_exact(fd, b, 8) <= 0) return -2;
		*val_out = ((uint64_t)b[0] << 56) | ((uint64_t)b[1] << 48) |
			   ((uint64_t)b[2] << 40) | ((uint64_t)b[3] << 32) |
			   ((uint64_t)b[4] << 24) | ((uint64_t)b[5] << 16) |
			   ((uint64_t)b[6] << 8) | b[7];
	} else {
		return -2; /* indefinite / reserved */
	}

	return 0;
}

/* Expect a uint (major 0), returns 0 on success */
static int cbor_read_uint(int fd, uint64_t *val_out)
{
	uint8_t major;
	int ret = cbor_read_head(fd, &major, val_out);
	if (ret) return ret;
	if (major != 0) return -2;
	return 0;
}

/* Read a text string (major 3) into buf, NUL-terminated. Returns 0 on success */
static int cbor_read_text(int fd, char *buf, size_t cap, uint64_t *len_out)
{
	uint8_t major;
	uint64_t len;
	int ret = cbor_read_head(fd, &major, &len);
	if (ret) return ret;
	if (major != 3) return -2;
	if (len >= cap) return -2; /* too long */
	if (len > 0 && read_exact(fd, buf, len) <= 0) return -2;
	buf[len] = '\0';
	if (len_out) *len_out = len;
	return 0;
}

/* Read a byte string header (major 2), returns length */
static int cbor_read_bytes_hdr(int fd, uint64_t *len_out)
{
	uint8_t major;
	int ret = cbor_read_head(fd, &major, len_out);
	if (ret) return ret;
	if (major != 2) return -2;
	return 0;
}

/* Read a map header (major 5), returns count */
static int cbor_read_map(int fd, uint64_t *count_out)
{
	uint8_t major;
	int ret = cbor_read_head(fd, &major, count_out);
	if (ret) return ret;
	if (major != 5) return -2;
	return 0;
}

/* Skip one CBOR data item (recursively handles maps, arrays, etc.) */
static int cbor_skip(int fd)
{
	uint8_t major;
	uint64_t val;
	int ret = cbor_read_head(fd, &major, &val);
	if (ret) return ret;

	switch (major) {
	case 0: /* uint */
	case 1: /* negint */
	case 7: /* simple/float */
		return 0;
	case 2: /* byte string */
	case 3: /* text string */
		if (val > 0) {
			/* Skip val bytes */
			while (val > 0) {
				uint8_t tmp[256];
				size_t chunk = val < sizeof(tmp) ? (size_t)val : sizeof(tmp);
				if (read_exact(fd, tmp, chunk) <= 0) return -2;
				val -= chunk;
			}
		}
		return 0;
	case 4: /* array */
		for (uint64_t i = 0; i < val; i++) {
			if (cbor_skip(fd)) return -2;
		}
		return 0;
	case 5: /* map */
		for (uint64_t i = 0; i < val; i++) {
			if (cbor_skip(fd)) return -2; /* key */
			if (cbor_skip(fd)) return -2; /* value */
		}
		return 0;
	default:
		return -2;
	}
}

/* ----------------------------------------------------------------
 * Log reading (CBOR format)
 * ---------------------------------------------------------------- */

static int cmd_log_cbor(int fd, const char *path, int json, int show_data)
{
	uint64_t map_count, key, val;
	uint32_t version = 0, block_size = 4096;
	char segment_label[32];
	uint64_t device_size = 0;
	char backing_path[LBD_LOG_PATH_MAX];
	int entry_count = 0;
	char sizebuf[32];
	uint8_t *data = NULL;
	size_t data_cap = 0;

	memset(segment_label, 0, sizeof(segment_label));
	memset(backing_path, 0, sizeof(backing_path));

	/* Read header map */
	if (cbor_read_map(fd, &map_count)) {
		fprintf(stderr, "Failed to read CBOR header map\n");
		return 1;
	}

	for (uint64_t i = 0; i < map_count; i++) {
		if (cbor_read_uint(fd, &key)) {
			fprintf(stderr, "Failed to read header key\n");
			return 1;
		}
		switch (key) {
		case LBD_CBOR_KEY_HDR_VERSION:
			if (cbor_read_uint(fd, &val)) return 1;
			version = (uint32_t)val;
			break;
		case LBD_CBOR_KEY_HDR_BLOCK_SIZE:
			if (cbor_read_uint(fd, &val)) return 1;
			block_size = (uint32_t)val;
			break;
		case LBD_CBOR_KEY_HDR_SEGMENT_LABEL:
			if (cbor_read_text(fd, segment_label,
					   sizeof(segment_label), NULL))
				return 1;
			break;
		case LBD_CBOR_KEY_HDR_DEVICE_SIZE:
			if (cbor_read_uint(fd, &val)) return 1;
			device_size = val;
			break;
		case LBD_CBOR_KEY_HDR_BACKING_PATH:
			if (cbor_read_text(fd, backing_path, sizeof(backing_path), NULL))
				return 1;
			break;
		default:
			if (cbor_skip(fd)) return 1;
			break;
		}
	}

	if (json) {
		printf("{\n");
		printf("  \"header\": {\n");
		printf("    \"version\": %u,\n", version);
		printf("    \"block_size\": %u,\n", block_size);
		printf("    \"segment_label\": ");
		json_print_string(stdout, segment_label);
		printf(",\n");
		printf("    \"device_size\": %llu,\n",
		       (unsigned long long)device_size);
		printf("    \"backing_path\": ");
		json_print_string(stdout, backing_path);
		printf("\n  },\n");
		printf("  \"entries\": [\n");
	} else {
		printf("=== LBD Log: %s ===\n", path);
		printf("Version:      %u\n", version);
		printf("Block size:   %u\n", block_size);
		printf("Segment:      %s\n", segment_label);
		printf("Device size:  %s (%llu bytes)\n",
		       fmt_size(device_size, sizebuf, sizeof(sizebuf)),
		       (unsigned long long)device_size);
		printf("Backing file: %s\n", backing_path);
		printf("\n");
	}

	/* Read entry maps until EOF */
	while (1) {
		uint8_t peek_major;
		uint64_t entry_map_count;
		int ret;

		/* Try to read next map header; EOF is normal end */
		ret = cbor_read_head(fd, &peek_major, &entry_map_count);
		if (ret == -1)
			break; /* clean EOF */
		if (ret < 0) {
			fprintf(stderr, "Error reading entry %d\n", entry_count);
			break;
		}
		if (peek_major != 5) {
			fprintf(stderr, "Expected CBOR map at entry %d, got major %u\n",
				entry_count, peek_major);
			break;
		}

		/* Parse entry fields */
		char op = 0;
		uint64_t timestamp_ns = 0, sequence = 0, block = 0;
		uint32_t length = 0, checksum = 0;
		int has_checksum = 0, has_data = 0;
		uint64_t data_len = 0;
		uint64_t comp_size = 0; /* compressed size from CBOR */

		for (uint64_t i = 0; i < entry_map_count; i++) {
			if (cbor_read_uint(fd, &key)) {
				fprintf(stderr, "Failed to read entry key at entry %d\n",
					entry_count);
				goto done;
			}
			switch (key) {
			case LBD_CBOR_KEY_OP: {
				char tbuf[4];
				if (cbor_read_text(fd, tbuf, sizeof(tbuf), NULL))
					goto done;
				op = tbuf[0];
				break;
			}
			case LBD_CBOR_KEY_TIMESTAMP:
				if (cbor_read_uint(fd, &timestamp_ns)) goto done;
				break;
			case LBD_CBOR_KEY_SEQUENCE:
				if (cbor_read_uint(fd, &sequence)) goto done;
				break;
			case LBD_CBOR_KEY_BLOCK:
				if (cbor_read_uint(fd, &block)) goto done;
				break;
			case LBD_CBOR_KEY_LENGTH:
				if (cbor_read_uint(fd, &val)) goto done;
				length = (uint32_t)val;
				break;
			case LBD_CBOR_KEY_CHECKSUM:
				if (cbor_read_uint(fd, &val)) goto done;
				checksum = (uint32_t)val;
				has_checksum = 1;
				break;
			case LBD_CBOR_KEY_DATA:
				if (cbor_read_bytes_hdr(fd, &data_len)) goto done;
				has_data = 1;
				if (data_len > data_cap) {
					free(data);
					data_cap = (size_t)data_len;
					data = malloc(data_cap);
					if (!data) {
						fprintf(stderr,
							"Out of memory for %llu byte payload\n",
							(unsigned long long)data_len);
						goto done;
					}
				}
				if (data_len > 0 &&
				    read_exact(fd, data, (size_t)data_len) <= 0) {
					fprintf(stderr,
						"Truncated data at entry %d\n",
						entry_count);
					goto done;
				}
				break;
			default:
				if (cbor_skip(fd)) goto done;
				break;
			}
		}

		int is_trim = (op == 'T');

		/* Decompress LZ4 data */
		comp_size = data_len;
		if (has_data && length > 0 && data_len > 0) {
			uint8_t *decompressed = malloc(length);
			if (!decompressed) {
				fprintf(stderr,
					"Out of memory for %u byte decompression buffer\n",
					length);
				goto done;
			}
			int dec_len = LZ4_decompress_safe(
				(const char *)data, (char *)decompressed,
				(int)data_len, (int)length);
			if (dec_len < 0) {
				fprintf(stderr,
					"LZ4 decompression failed at entry %d\n",
					entry_count);
				free(decompressed);
				goto done;
			}
			/* Replace compressed data with decompressed */
			if ((size_t)length > data_cap) {
				free(data);
				data_cap = length;
				data = malloc(data_cap);
				if (!data) {
					free(decompressed);
					data = NULL;
					data_cap = 0;
					goto done;
				}
			}
			memcpy(data, decompressed, dec_len);
			data_len = dec_len;
			free(decompressed);
		}

		/* Validate CRC on decompressed data */
		uint32_t computed_crc = 0;
		int crc_ok = 1;
		if (has_data && has_checksum) {
			computed_crc = crc32_calc(data, (size_t)data_len);
			crc_ok = (computed_crc == checksum);
		}

		if (json) {
			if (entry_count > 0)
				printf(",\n");
			printf("    {\n");
			printf("      \"type\": \"%s\",\n",
			       is_trim ? "trim" : "write");
			printf("      \"sequence\": %llu,\n",
			       (unsigned long long)sequence);
			printf("      \"timestamp_ns\": %llu,\n",
			       (unsigned long long)timestamp_ns);

			time_t secs = timestamp_ns / 1000000000ULL;
			unsigned long ns = timestamp_ns % 1000000000ULL;
			struct tm tm;
			gmtime_r(&secs, &tm);
			char tbuf[64];
			strftime(tbuf, sizeof(tbuf), "%Y-%m-%dT%H:%M:%S", &tm);
			printf("      \"timestamp\": \"%s.%09luZ\",\n", tbuf, ns);

			printf("      \"block\": %llu,\n",
			       (unsigned long long)block);
			printf("      \"block_end\": %llu,\n",
			       (unsigned long long)(block + length / block_size - 1));
			printf("      \"extent\": \"%llu-%llu\",\n",
			       (unsigned long long)block,
			       (unsigned long long)(block + length / block_size - 1));
			printf("      \"offset_bytes\": %llu,\n",
			       (unsigned long long)block * block_size);
			printf("      \"length\": %u", length);
			if (has_data && comp_size > 0) {
				printf(",\n      \"compressed_size\": %llu",
				       (unsigned long long)comp_size);
				printf(",\n      \"compression_ratio\": %.1f",
				       length > 0 ? (1.0 - (double)comp_size / length) * 100.0 : 0.0);
			}
			if (has_checksum) {
				printf(",\n      \"checksum\": \"0x%08x\",\n",
				       checksum);
				printf("      \"checksum_valid\": %s",
				       crc_ok ? "true" : "false");
			}
			if (show_data && has_data) {
				printf(",\n      \"data_hex\": \"");
				for (uint64_t i = 0; i < data_len; i++)
					printf("%02x", data[i]);
				printf("\"");
			}
			printf("\n    }");
		} else {
			time_t secs = timestamp_ns / 1000000000ULL;
			unsigned long ns = timestamp_ns % 1000000000ULL;
			struct tm tm;
			gmtime_r(&secs, &tm);
			char tbuf[64];
			strftime(tbuf, sizeof(tbuf), "%Y-%m-%d %H:%M:%S", &tm);

			printf("--- Entry #%llu [%s] ---\n",
			       (unsigned long long)sequence,
			       is_trim ? "TRIM" : "WRITE");
			printf("  Time:     %s.%09lu UTC\n", tbuf, ns);
			printf("  Extent:   %llu-%llu (%u blocks)\n",
			       (unsigned long long)block,
			       (unsigned long long)(block + length / block_size - 1),
			       length / block_size);
			printf("  Offset:   0x%llx-0x%llx\n",
			       (unsigned long long)block * block_size,
			       (unsigned long long)(block * block_size + length - 1));
			printf("  Length:   %s (%u bytes)\n",
			       fmt_size(length, sizebuf, sizeof(sizebuf)),
			       length);
			if (has_data && comp_size > 0) {
				printf("  LZ4:      %s compressed (%.1f%% reduction)\n",
				       fmt_size(comp_size, sizebuf, sizeof(sizebuf)),
				       length > 0 ? (1.0 - (double)comp_size / length) * 100.0 : 0.0);
			}
			if (has_checksum) {
				printf("  CRC32:    0x%08x %s\n", checksum,
				       crc_ok ? "(OK)" : "(MISMATCH - computed 0x%08x)");
				if (!crc_ok)
					printf("            computed: 0x%08x\n",
					       computed_crc);
			}
			if (show_data && has_data) {
				printf("  Data:\n");
				hex_dump(stdout, data, (size_t)data_len, "    ");
			}
			printf("\n");
		}

		entry_count++;
	}

done:
	if (json) {
		printf("\n  ],\n");
		printf("  \"entry_count\": %d\n", entry_count);
		printf("}\n");
	} else {
		printf("Total entries: %d\n", entry_count);
	}

	free(data);
	return 0;
}

static int cmd_log(const char *path, int json, int show_data)
{
	int fd, ret;

	fd = open(path, O_RDONLY);
	if (fd < 0) {
		fprintf(stderr, "Cannot open log file '%s': %s\n",
			path, strerror(errno));
		return 1;
	}

	ret = cmd_log_cbor(fd, path, json, show_data);
	close(fd);
	return ret;
}

/* ----------------------------------------------------------------
 * qcow2-lz4 format support
 * ---------------------------------------------------------------- */

#include "lbd_qcow2_format.h"

static inline uint64_t htobe64_val(uint64_t x)
{
	uint8_t buf[8];
	_qcow2_put64(buf, 0, x);
	uint64_t v;
	memcpy(&v, buf, 8);
	return v;
}

static inline uint64_t be64toh_val(uint64_t x)
{
	uint8_t buf[8];
	memcpy(buf, &x, 8);
	return _qcow2_get64(buf, 0);
}

static inline uint32_t htobe32_val(uint32_t x)
{
	uint8_t buf[4];
	_qcow2_put32(buf, 0, x);
	uint32_t v;
	memcpy(&v, buf, 4);
	return v;
}

static inline uint32_t be32toh_val(uint32_t x)
{
	uint8_t buf[4];
	memcpy(buf, &x, 4);
	return _qcow2_get32(buf, 0);
}

static int is_all_zero(const void *buf, size_t len)
{
	const uint8_t *p = buf;
	for (size_t i = 0; i < len; i++)
		if (p[i] != 0)
			return 0;
	return 1;
}

static uint64_t parse_size(const char *s)
{
	char *end;
	uint64_t val = strtoull(s, &end, 0);

	switch (*end) {
	case 'k': case 'K': val *= 1024; break;
	case 'm': case 'M': val *= 1024 * 1024; break;
	case 'g': case 'G': val *= 1024ULL * 1024 * 1024; break;
	case 't': case 'T': val *= 1024ULL * 1024 * 1024 * 1024; break;
	}
	return val;
}

/*
 * lbdctl create --size <bytes> [--cluster-bits 16] <path>
 * Create an empty qcow2-lz4 image file.
 */
static int cmd_create(int argc, char **argv)
{
	const char *path = NULL;
	uint64_t virtual_size = 0;
	uint32_t cluster_bits = 16;
	uint32_t cluster_size, l2_entries, l1_size;
	uint64_t l1_table_offset, alloc_offset;
	uint8_t hdr[LBD_QCOW2_HEADER_SIZE];
	int fd;
	ssize_t n;

	for (int i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--size") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--size requires a value\n");
				return 1;
			}
			virtual_size = parse_size(argv[i]);
		} else if (strcmp(argv[i], "--cluster-bits") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--cluster-bits requires a value\n");
				return 1;
			}
			cluster_bits = atoi(argv[i]);
		} else if (!path) {
			path = argv[i];
		} else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	if (!path) {
		fprintf(stderr, "create requires an output path\n");
		return 1;
	}

	if (virtual_size == 0) {
		fprintf(stderr, "create requires --size <bytes>\n");
		return 1;
	}

	if (cluster_bits < 12 || cluster_bits > 24) {
		fprintf(stderr, "cluster_bits must be between 12 and 24\n");
		return 1;
	}

	cluster_size = 1U << cluster_bits;
	l2_entries = (cluster_size - LBD_QCOW2_L2_TRAILER_SIZE) / 8;

	/* Compute L1 size: one entry per L2 table needed */
	l1_size = (virtual_size + (uint64_t)l2_entries * cluster_size - 1) /
		  ((uint64_t)l2_entries * cluster_size);
	if (l1_size == 0)
		l1_size = 1;

	l1_table_offset = LBD_QCOW2_HEADER_SIZE;
	alloc_offset = l1_table_offset + (uint64_t)l1_size * sizeof(uint64_t);
	/* Align alloc_offset to cluster boundary */
	alloc_offset = (alloc_offset + cluster_size - 1) & ~((uint64_t)cluster_size - 1);

	/* Build header */
	memset(hdr, 0, LBD_QCOW2_HEADER_SIZE);
	lbd_qcow2_hdr_set_magic(hdr, LBD_QCOW2_MAGIC);
	lbd_qcow2_hdr_set_version(hdr, LBD_QCOW2_VERSION);
	lbd_qcow2_hdr_set_cluster_bits(hdr, cluster_bits);
	lbd_qcow2_hdr_set_virtual_size(hdr, virtual_size);
	lbd_qcow2_hdr_set_l1_table_offset(hdr, l1_table_offset);
	lbd_qcow2_hdr_set_l1_size(hdr, l1_size);
	lbd_qcow2_hdr_set_alloc_offset(hdr, alloc_offset);
	lbd_qcow2_hdr_set_comp_type(hdr, LBD_QCOW2_COMP_LZ4);
	lbd_qcow2_hdr_set_free_list(hdr, 0);

	fd = open(path, O_RDWR | O_CREAT | O_TRUNC, 0644);
	if (fd < 0) {
		fprintf(stderr, "Cannot create '%s': %s\n", path, strerror(errno));
		return 1;
	}

	/* Write header */
	n = write(fd, hdr, LBD_QCOW2_HEADER_SIZE);
	if (n != LBD_QCOW2_HEADER_SIZE) {
		fprintf(stderr, "Failed to write header: %s\n", strerror(errno));
		close(fd);
		return 1;
	}

	/* Write zeroed L1 table */
	{
		size_t l1_bytes = l1_size * sizeof(uint64_t);
		void *zeros = calloc(1, l1_bytes);
		if (!zeros) {
			fprintf(stderr, "Out of memory\n");
			close(fd);
			return 1;
		}
		n = write(fd, zeros, l1_bytes);
		free(zeros);
		if (n != (ssize_t)l1_bytes) {
			fprintf(stderr, "Failed to write L1 table: %s\n",
				strerror(errno));
			close(fd);
			return 1;
		}
	}

	/* Extend file to alloc_offset */
	if (ftruncate(fd, alloc_offset) < 0) {
		fprintf(stderr, "Failed to extend file: %s\n", strerror(errno));
		close(fd);
		return 1;
	}

	close(fd);

	printf("Created qcow2-lz4 image: %s\n", path);
	printf("  Virtual size:  %llu bytes (%s)\n",
	       (unsigned long long)virtual_size,
	       fmt_size(virtual_size, (char[32]){0}, 32));
	printf("  Cluster size:  %u bytes (bits=%u)\n", cluster_size, cluster_bits);
	printf("  L1 entries:    %u\n", l1_size);
	printf("  L2 entries:    %u per table\n", l2_entries);
	printf("  Alloc offset:  %llu\n", (unsigned long long)alloc_offset);

	return 0;
}

/*
 * lbdctl convert <flat-file> <qcow2-file>
 * Convert a flat (raw) image file to qcow2-lz4 format.
 */
static int cmd_convert(const char *flat_path, const char *qcow2_path)
{
	int in_fd, out_fd;
	struct stat st;
	uint64_t virtual_size;
	uint32_t cluster_bits = 16;
	uint32_t cluster_size = 1U << cluster_bits;
	uint32_t l2_entries = (cluster_size - LBD_QCOW2_L2_TRAILER_SIZE) / 8;
	uint32_t l1_size;
	uint64_t l1_table_offset, alloc_offset;
	uint8_t hdr[LBD_QCOW2_HEADER_SIZE];
	uint64_t *l1_table;
	uint64_t **l2_tables;
	uint8_t *cluster_buf, *comp_buf;
	int comp_cap;
	uint64_t total_clusters, clusters_written = 0, clusters_zero = 0;
	ssize_t n;

	in_fd = open(flat_path, O_RDONLY);
	if (in_fd < 0) {
		fprintf(stderr, "Cannot open '%s': %s\n", flat_path, strerror(errno));
		return 1;
	}

	if (fstat(in_fd, &st) < 0) {
		fprintf(stderr, "Cannot stat '%s': %s\n", flat_path, strerror(errno));
		close(in_fd);
		return 1;
	}

	virtual_size = st.st_size;
	if (virtual_size == 0) {
		fprintf(stderr, "Input file is empty\n");
		close(in_fd);
		return 1;
	}

	total_clusters = (virtual_size + cluster_size - 1) / cluster_size;
	l1_size = (virtual_size + (uint64_t)l2_entries * cluster_size - 1) /
		  ((uint64_t)l2_entries * cluster_size);
	if (l1_size == 0)
		l1_size = 1;

	l1_table_offset = LBD_QCOW2_HEADER_SIZE;
	alloc_offset = l1_table_offset + (uint64_t)l1_size * sizeof(uint64_t);
	alloc_offset = (alloc_offset + cluster_size - 1) & ~((uint64_t)cluster_size - 1);

	/* Allocate tables */
	l1_table = calloc(l1_size, sizeof(uint64_t));
	l2_tables = calloc(l1_size, sizeof(uint64_t *));
	cluster_buf = malloc(cluster_size);
	comp_cap = LZ4_compressBound(cluster_size);
	comp_buf = malloc(comp_cap);

	if (!l1_table || !l2_tables || !cluster_buf || !comp_buf) {
		fprintf(stderr, "Out of memory\n");
		close(in_fd);
		return 1;
	}

	for (uint32_t i = 0; i < l1_size; i++) {
		l2_tables[i] = calloc(l2_entries, sizeof(uint64_t));
		if (!l2_tables[i]) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			return 1;
		}
	}

	out_fd = open(qcow2_path, O_RDWR | O_CREAT | O_TRUNC, 0644);
	if (out_fd < 0) {
		fprintf(stderr, "Cannot create '%s': %s\n", qcow2_path, strerror(errno));
		close(in_fd);
		return 1;
	}

	/* Reserve space for header + L1 table (written at end) */
	if (ftruncate(out_fd, alloc_offset) < 0) {
		fprintf(stderr, "Failed to extend output: %s\n", strerror(errno));
		close(in_fd);
		close(out_fd);
		return 1;
	}
	if (lseek(out_fd, alloc_offset, SEEK_SET) < 0) {
		fprintf(stderr, "Failed to seek: %s\n", strerror(errno));
		close(in_fd);
		close(out_fd);
		return 1;
	}

	/* Process each cluster */
	for (uint64_t ci = 0; ci < total_clusters; ci++) {
		uint32_t l1_idx = ci / l2_entries;
		uint32_t l2_idx = ci % l2_entries;
		size_t to_read = cluster_size;
		ssize_t rd;

		/* Handle last partial cluster */
		if ((ci + 1) * cluster_size > virtual_size)
			to_read = virtual_size - ci * cluster_size;

		memset(cluster_buf, 0, cluster_size);
		rd = pread(in_fd, cluster_buf, to_read, ci * cluster_size);
		if (rd < 0) {
			fprintf(stderr, "Read error at cluster %llu: %s\n",
				(unsigned long long)ci, strerror(errno));
			close(in_fd);
			close(out_fd);
			return 1;
		}

		/* Skip all-zero clusters (leave L2 = 0 for sparse) */
		if (is_all_zero(cluster_buf, cluster_size)) {
			clusters_zero++;
			continue;
		}

		/* Allocate L2 table if needed */
		if (l1_table[l1_idx] == 0) {
			l1_table[l1_idx] = alloc_offset;
			alloc_offset += cluster_size;
		}

		/* Compress */
		int comp_len = LZ4_compress_default(
			(const char *)cluster_buf, (char *)comp_buf,
			cluster_size, comp_cap);

		if (comp_len > 0 &&
		    (uint32_t)comp_len < cluster_size - sizeof(uint32_t)) {
			/* Store compressed */
			uint32_t total_on_disk = ((sizeof(uint32_t) + comp_len) + 4095) & ~4095U;
			uint64_t phys = alloc_offset;
			uint32_t comp_size_be = htobe32_val(comp_len);

			/* Write size header + compressed data */
			if (pwrite(out_fd, &comp_size_be, sizeof(comp_size_be), phys) !=
			    sizeof(comp_size_be)) {
				fprintf(stderr, "Write error\n");
				close(in_fd);
				close(out_fd);
				return 1;
			}
			if (pwrite(out_fd, comp_buf, comp_len,
				   phys + sizeof(comp_size_be)) != comp_len) {
				fprintf(stderr, "Write error\n");
				close(in_fd);
				close(out_fd);
				return 1;
			}

			l2_tables[l1_idx][l2_idx] = LBD_QCOW2_L2_COMPRESSED | phys;
			alloc_offset += total_on_disk;
		} else {
			/* Store uncompressed */
			uint64_t phys = alloc_offset;

			if (pwrite(out_fd, cluster_buf, cluster_size, phys) !=
			    cluster_size) {
				fprintf(stderr, "Write error\n");
				close(in_fd);
				close(out_fd);
				return 1;
			}

			l2_tables[l1_idx][l2_idx] = phys;
			alloc_offset += cluster_size;
		}

		clusters_written++;
	}

	/* Write L2 tables to their allocated positions */
	for (uint32_t i = 0; i < l1_size; i++) {
		if (l1_table[i] == 0)
			continue;

		/* Convert L2 entries to big-endian on disk */
		uint64_t *disk_l2 = malloc(cluster_size);
		if (!disk_l2) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			close(out_fd);
			return 1;
		}

		memset(disk_l2, 0, cluster_size);
		for (uint32_t j = 0; j < l2_entries; j++)
			disk_l2[j] = htobe64_val(l2_tables[i][j]);

		/* Compute and store CRC32C trailer */
		{
			uint8_t *raw = (uint8_t *)disk_l2;
			uint32_t crc = crc32c_calc(raw, cluster_size - 4);
			_qcow2_put32(raw, cluster_size - 4, crc);
		}

		n = pwrite(out_fd, disk_l2, cluster_size, l1_table[i]);
		free(disk_l2);
		if (n != cluster_size) {
			fprintf(stderr, "Failed to write L2 table %u\n", i);
			close(in_fd);
			close(out_fd);
			return 1;
		}
	}

	/* Write header */
	memset(hdr, 0, LBD_QCOW2_HEADER_SIZE);
	lbd_qcow2_hdr_set_magic(hdr, LBD_QCOW2_MAGIC);
	lbd_qcow2_hdr_set_version(hdr, LBD_QCOW2_VERSION);
	lbd_qcow2_hdr_set_cluster_bits(hdr, cluster_bits);
	lbd_qcow2_hdr_set_virtual_size(hdr, virtual_size);
	lbd_qcow2_hdr_set_l1_table_offset(hdr, l1_table_offset);
	lbd_qcow2_hdr_set_l1_size(hdr, l1_size);
	lbd_qcow2_hdr_set_alloc_offset(hdr, alloc_offset);
	lbd_qcow2_hdr_set_comp_type(hdr, LBD_QCOW2_COMP_LZ4);
	lbd_qcow2_hdr_set_free_list(hdr, 0);

	n = pwrite(out_fd, hdr, LBD_QCOW2_HEADER_SIZE, 0);
	if (n != LBD_QCOW2_HEADER_SIZE) {
		fprintf(stderr, "Failed to write header\n");
		close(in_fd);
		close(out_fd);
		return 1;
	}

	/* Write L1 table (big-endian) */
	{
		uint64_t *disk_l1 = calloc(l1_size, sizeof(uint64_t));
		if (!disk_l1) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			close(out_fd);
			return 1;
		}
		for (uint32_t i = 0; i < l1_size; i++)
			disk_l1[i] = htobe64_val(l1_table[i]);

		n = pwrite(out_fd, disk_l1, l1_size * sizeof(uint64_t),
			   l1_table_offset);
		free(disk_l1);
		if (n != (ssize_t)(l1_size * sizeof(uint64_t))) {
			fprintf(stderr, "Failed to write L1 table\n");
			close(in_fd);
			close(out_fd);
			return 1;
		}
	}

	/* Truncate file to final size */
	if (ftruncate(out_fd, alloc_offset) < 0)
		fprintf(stderr, "Warning: ftruncate failed: %s\n", strerror(errno));

	close(in_fd);
	close(out_fd);

	/* Cleanup */
	for (uint32_t i = 0; i < l1_size; i++)
		free(l2_tables[i]);
	free(l2_tables);
	free(l1_table);
	free(cluster_buf);
	free(comp_buf);

	printf("Converted %s -> %s\n", flat_path, qcow2_path);
	printf("  Virtual size:     %llu bytes\n", (unsigned long long)virtual_size);
	printf("  Total clusters:   %llu\n", (unsigned long long)total_clusters);
	printf("  Written:          %llu (data)\n", (unsigned long long)clusters_written);
	printf("  Zero (sparse):    %llu\n", (unsigned long long)clusters_zero);

	{
		struct stat out_st;
		if (stat(qcow2_path, &out_st) == 0) {
			printf("  File size:        %llu bytes (%s)\n",
			       (unsigned long long)out_st.st_size,
			       fmt_size(out_st.st_size, (char[32]){0}, 32));
			if (virtual_size > 0)
				printf("  Compression:      %.1f%%\n",
				       (1.0 - (double)out_st.st_size / virtual_size) * 100.0);
		}
	}

	return 0;
}

/*
 * lbdctl extract <qcow2-file> <flat-file>
 * Decompress all clusters from a qcow2-lz4 image to a flat file.
 */
static int cmd_extract(const char *qcow2_path, const char *flat_path)
{
	int in_fd, out_fd;
	uint8_t hdr[LBD_QCOW2_HEADER_SIZE];
	uint64_t virtual_size, l1_table_offset, alloc_off;
	uint32_t cluster_bits, cluster_size, l2_entries, l1_size;
	uint64_t *l1_table;
	uint8_t *cluster_buf, *comp_buf;
	int comp_cap;
	ssize_t n;

	in_fd = open(qcow2_path, O_RDONLY);
	if (in_fd < 0) {
		fprintf(stderr, "Cannot open '%s': %s\n", qcow2_path, strerror(errno));
		return 1;
	}

	/* Read header */
	n = pread(in_fd, hdr, LBD_QCOW2_HEADER_SIZE, 0);
	if (n != LBD_QCOW2_HEADER_SIZE) {
		fprintf(stderr, "Failed to read header\n");
		close(in_fd);
		return 1;
	}

	if (lbd_qcow2_hdr_magic(hdr) != LBD_QCOW2_MAGIC) {
		fprintf(stderr, "Not a qcow2-lz4 image\n");
		close(in_fd);
		return 1;
	}

	cluster_bits = lbd_qcow2_hdr_cluster_bits(hdr);
	cluster_size = 1U << cluster_bits;
	l2_entries = (cluster_size - LBD_QCOW2_L2_TRAILER_SIZE) / 8;
	virtual_size = lbd_qcow2_hdr_virtual_size(hdr);
	l1_table_offset = lbd_qcow2_hdr_l1_table_offset(hdr);
	l1_size = lbd_qcow2_hdr_l1_size(hdr);
	alloc_off = lbd_qcow2_hdr_alloc_offset(hdr);
	(void)alloc_off;

	/* Read L1 table */
	l1_table = calloc(l1_size, sizeof(uint64_t));
	comp_cap = LZ4_compressBound(cluster_size);
	cluster_buf = malloc(cluster_size);
	comp_buf = malloc(comp_cap);

	if (!l1_table || !cluster_buf || !comp_buf) {
		fprintf(stderr, "Out of memory\n");
		close(in_fd);
		return 1;
	}

	{
		uint64_t *disk_l1 = malloc(l1_size * sizeof(uint64_t));
		if (!disk_l1) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			return 1;
		}
		n = pread(in_fd, disk_l1, l1_size * sizeof(uint64_t), l1_table_offset);
		if (n != (ssize_t)(l1_size * sizeof(uint64_t))) {
			fprintf(stderr, "Failed to read L1 table\n");
			free(disk_l1);
			close(in_fd);
			return 1;
		}
		for (uint32_t i = 0; i < l1_size; i++)
			l1_table[i] = be64toh_val(disk_l1[i]);
		free(disk_l1);
	}

	/* Create output file */
	out_fd = open(flat_path, O_RDWR | O_CREAT | O_TRUNC, 0644);
	if (out_fd < 0) {
		fprintf(stderr, "Cannot create '%s': %s\n", flat_path, strerror(errno));
		close(in_fd);
		return 1;
	}

	/* Pre-allocate the output */
	if (ftruncate(out_fd, virtual_size) < 0) {
		fprintf(stderr, "Failed to extend output: %s\n", strerror(errno));
		close(in_fd);
		close(out_fd);
		return 1;
	}

	/* Read and verify all L2 tables */
	uint64_t **ext_l2_tables = calloc(l1_size, sizeof(uint64_t *));
	if (!ext_l2_tables) {
		fprintf(stderr, "Out of memory\n");
		close(in_fd);
		close(out_fd);
		return 1;
	}
	for (uint32_t i = 0; i < l1_size; i++) {
		ext_l2_tables[i] = calloc(l2_entries, sizeof(uint64_t));
		if (!ext_l2_tables[i]) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			close(out_fd);
			return 1;
		}
		if (l1_table[i] == 0)
			continue;

		uint8_t *disk_l2 = malloc(cluster_size);
		if (!disk_l2) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			close(out_fd);
			return 1;
		}
		n = pread(in_fd, disk_l2, cluster_size, l1_table[i]);
		if (n != (ssize_t)cluster_size) {
			fprintf(stderr, "Failed to read L2 table %u\n", i);
			free(disk_l2);
			close(in_fd);
			close(out_fd);
			return 1;
		}

		/* Verify CRC32C */
		{
			uint32_t stored_crc = _qcow2_get32(disk_l2, cluster_size - 4);
			uint32_t calc_crc = crc32c_calc(disk_l2, cluster_size - 4);
			if (stored_crc != calc_crc) {
				fprintf(stderr, "L2 CRC32C mismatch for l1[%u]: "
					"stored=0x%08x computed=0x%08x\n",
					i, stored_crc, calc_crc);
				free(disk_l2);
				close(in_fd);
				close(out_fd);
				return 1;
			}
		}

		for (uint32_t j = 0; j < l2_entries; j++)
			ext_l2_tables[i][j] = be64toh_val(((uint64_t *)disk_l2)[j]);
		free(disk_l2);
	}

	/* Extract each cluster */
	uint64_t total_clusters = (virtual_size + cluster_size - 1) / cluster_size;

	for (uint64_t ci = 0; ci < total_clusters; ci++) {
		uint32_t l1_idx = ci / l2_entries;
		uint32_t l2_idx = ci % l2_entries;
		uint64_t l2_entry;
		uint64_t phys_offset;
		size_t write_len = cluster_size;

		if (l1_idx >= l1_size || l1_table[l1_idx] == 0) {
			/* Unallocated L2 table: output zeros (already zero from ftruncate) */
			continue;
		}

		l2_entry = ext_l2_tables[l1_idx][l2_idx];

		if (l2_entry == 0) {
			/* Unallocated cluster: zeros */
			continue;
		}

		phys_offset = l2_entry & LBD_QCOW2_L2_OFFSET_MASK;

		/* Handle last partial cluster */
		if ((ci + 1) * cluster_size > virtual_size)
			write_len = virtual_size - ci * cluster_size;

		if (l2_entry & LBD_QCOW2_L2_COMPRESSED) {
			/* Compressed cluster */
			uint32_t comp_size_be, comp_size;

			n = pread(in_fd, &comp_size_be, sizeof(comp_size_be),
				  phys_offset);
			if (n != sizeof(comp_size_be)) {
				fprintf(stderr, "Failed to read compressed size at cluster %llu\n",
					(unsigned long long)ci);
				close(in_fd);
				close(out_fd);
				return 1;
			}

			comp_size = be32toh_val(comp_size_be);

			n = pread(in_fd, comp_buf, comp_size,
				  phys_offset + sizeof(comp_size_be));
			if (n != (ssize_t)comp_size) {
				fprintf(stderr, "Failed to read compressed data at cluster %llu\n",
					(unsigned long long)ci);
				close(in_fd);
				close(out_fd);
				return 1;
			}

			int dec_len = LZ4_decompress_safe(
				(const char *)comp_buf, (char *)cluster_buf,
				comp_size, cluster_size);
			if (dec_len != (int)cluster_size) {
				fprintf(stderr, "LZ4 decompression failed at cluster %llu (got %d, expected %u)\n",
					(unsigned long long)ci, dec_len, cluster_size);
				close(in_fd);
				close(out_fd);
				return 1;
			}
		} else {
			/* Uncompressed cluster */
			n = pread(in_fd, cluster_buf, cluster_size, phys_offset);
			if (n != (ssize_t)cluster_size) {
				fprintf(stderr, "Failed to read cluster %llu\n",
					(unsigned long long)ci);
				close(in_fd);
				close(out_fd);
				return 1;
			}
		}

		n = pwrite(out_fd, cluster_buf, write_len, ci * cluster_size);
		if (n != (ssize_t)write_len) {
			fprintf(stderr, "Failed to write cluster %llu\n",
				(unsigned long long)ci);
			close(in_fd);
			close(out_fd);
			return 1;
		}
	}

	close(in_fd);
	close(out_fd);
	for (uint32_t i = 0; i < l1_size; i++)
		free(ext_l2_tables[i]);
	free(ext_l2_tables);
	free(l1_table);
	free(cluster_buf);
	free(comp_buf);

	printf("Extracted %s -> %s (%llu bytes)\n",
	       qcow2_path, flat_path, (unsigned long long)virtual_size);

	return 0;
}

/*
 * lbdctl compact [--inplace] <qcow2-file>
 * Rewrite the image, eliminating dead space from overwrites.
 */
static int cmd_compact(int argc, char **argv)
{
	const char *path = NULL;
	int in_fd, out_fd;
	uint8_t hdr[LBD_QCOW2_HEADER_SIZE];
	uint64_t virtual_size, l1_table_offset;
	uint32_t cluster_bits, cluster_size, l2_entries, l1_size;
	uint64_t *l1_table;
	uint64_t **l2_tables;
	uint8_t *cluster_buf, *comp_buf;
	int comp_cap;
	uint64_t new_alloc_offset;
	ssize_t n;
	char tmp_path[PATH_MAX];

	for (int i = 0; i < argc; i++) {
		if (!path)
			path = argv[i];
		else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	if (!path) {
		fprintf(stderr, "compact requires a qcow2 file path\n");
		return 1;
	}

	in_fd = open(path, O_RDONLY);
	if (in_fd < 0) {
		fprintf(stderr, "Cannot open '%s': %s\n", path, strerror(errno));
		return 1;
	}

	/* Read header */
	n = pread(in_fd, hdr, LBD_QCOW2_HEADER_SIZE, 0);
	if (n != LBD_QCOW2_HEADER_SIZE) {
		fprintf(stderr, "Failed to read header\n");
		close(in_fd);
		return 1;
	}

	if (lbd_qcow2_hdr_magic(hdr) != LBD_QCOW2_MAGIC) {
		fprintf(stderr, "Not a qcow2-lz4 image\n");
		close(in_fd);
		return 1;
	}

	cluster_bits = lbd_qcow2_hdr_cluster_bits(hdr);
	cluster_size = 1U << cluster_bits;
	l2_entries = (cluster_size - LBD_QCOW2_L2_TRAILER_SIZE) / 8;
	virtual_size = lbd_qcow2_hdr_virtual_size(hdr);
	l1_table_offset = lbd_qcow2_hdr_l1_table_offset(hdr);
	l1_size = lbd_qcow2_hdr_l1_size(hdr);

	/* Read L1 */
	l1_table = calloc(l1_size, sizeof(uint64_t));
	l2_tables = calloc(l1_size, sizeof(uint64_t *));
	cluster_buf = malloc(cluster_size);
	comp_cap = LZ4_compressBound(cluster_size);
	comp_buf = malloc(comp_cap);

	if (!l1_table || !l2_tables || !cluster_buf || !comp_buf) {
		fprintf(stderr, "Out of memory\n");
		close(in_fd);
		return 1;
	}

	{
		uint64_t *disk_l1 = malloc(l1_size * sizeof(uint64_t));
		if (!disk_l1) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			return 1;
		}
		n = pread(in_fd, disk_l1, l1_size * sizeof(uint64_t), l1_table_offset);
		if (n != (ssize_t)(l1_size * sizeof(uint64_t))) {
			fprintf(stderr, "Failed to read L1 table\n");
			free(disk_l1);
			close(in_fd);
			return 1;
		}
		for (uint32_t i = 0; i < l1_size; i++)
			l1_table[i] = be64toh_val(disk_l1[i]);
		free(disk_l1);
	}

	/* Read all L2 tables */
	for (uint32_t i = 0; i < l1_size; i++) {
		l2_tables[i] = calloc(l2_entries, sizeof(uint64_t));
		if (!l2_tables[i]) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			return 1;
		}

		if (l1_table[i] == 0)
			continue;

		uint64_t *disk_l2 = malloc(cluster_size);
		if (!disk_l2) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			return 1;
		}
		n = pread(in_fd, disk_l2, cluster_size, l1_table[i]);
		if (n != (ssize_t)cluster_size) {
			fprintf(stderr, "Failed to read L2 table %u\n", i);
			free(disk_l2);
			close(in_fd);
			return 1;
		}

		/* Verify CRC32C */
		{
			uint8_t *raw = (uint8_t *)disk_l2;
			uint32_t stored_crc = _qcow2_get32(raw, cluster_size - 4);
			uint32_t calc_crc = crc32c_calc(raw, cluster_size - 4);
			if (stored_crc != calc_crc) {
				fprintf(stderr, "L2 CRC32C mismatch for l1[%u]: "
					"stored=0x%08x computed=0x%08x\n",
					i, stored_crc, calc_crc);
				free(disk_l2);
				close(in_fd);
				return 1;
			}
		}

		for (uint32_t j = 0; j < l2_entries; j++)
			l2_tables[i][j] = be64toh_val(disk_l2[j]);
		free(disk_l2);
	}

	/* Create temp output file */
	snprintf(tmp_path, sizeof(tmp_path), "%s.compact.tmp", path);
	out_fd = open(tmp_path, O_RDWR | O_CREAT | O_TRUNC, 0644);
	if (out_fd < 0) {
		fprintf(stderr, "Cannot create temp file: %s\n", strerror(errno));
		close(in_fd);
		return 1;
	}

	/* New layout: header + L1 table, then data */
	new_alloc_offset = l1_table_offset + (uint64_t)l1_size * sizeof(uint64_t);
	new_alloc_offset = (new_alloc_offset + cluster_size - 1) & ~((uint64_t)cluster_size - 1);

	/* Reserve space for header + L1 */
	if (ftruncate(out_fd, new_alloc_offset) < 0) {
		fprintf(stderr, "Failed to extend temp file\n");
		close(in_fd);
		close(out_fd);
		return 1;
	}

	/* New L1 and L2 tables */
	uint64_t *new_l1 = calloc(l1_size, sizeof(uint64_t));
	uint64_t **new_l2 = calloc(l1_size, sizeof(uint64_t *));
	if (!new_l1 || !new_l2) {
		fprintf(stderr, "Out of memory\n");
		close(in_fd);
		close(out_fd);
		return 1;
	}
	for (uint32_t i = 0; i < l1_size; i++) {
		new_l2[i] = calloc(l2_entries, sizeof(uint64_t));
		if (!new_l2[i]) {
			fprintf(stderr, "Out of memory\n");
			close(in_fd);
			close(out_fd);
			return 1;
		}
	}

	/* Walk all clusters and re-pack */
	uint64_t total_clusters = (virtual_size + cluster_size - 1) / cluster_size;

	for (uint64_t ci = 0; ci < total_clusters; ci++) {
		uint32_t l1_idx = ci / l2_entries;
		uint32_t l2_idx = ci % l2_entries;
		uint64_t l2_entry;
		uint64_t phys_offset;

		if (l1_idx >= l1_size || l1_table[l1_idx] == 0)
			continue;

		l2_entry = l2_tables[l1_idx][l2_idx];
		if (l2_entry == 0)
			continue;

		phys_offset = l2_entry & LBD_QCOW2_L2_OFFSET_MASK;

		/* Read cluster data (decompress if needed) */
		if (l2_entry & LBD_QCOW2_L2_COMPRESSED) {
			uint32_t comp_size_be, comp_size;

			n = pread(in_fd, &comp_size_be, sizeof(comp_size_be), phys_offset);
			if (n != sizeof(comp_size_be)) {
				fprintf(stderr, "Read error\n");
				goto compact_err;
			}
			comp_size = be32toh_val(comp_size_be);

			n = pread(in_fd, comp_buf, comp_size,
				  phys_offset + sizeof(comp_size_be));
			if (n != (ssize_t)comp_size) {
				fprintf(stderr, "Read error\n");
				goto compact_err;
			}

			int dec_len = LZ4_decompress_safe(
				(const char *)comp_buf, (char *)cluster_buf,
				comp_size, cluster_size);
			if (dec_len != (int)cluster_size) {
				fprintf(stderr, "Decompression failed at cluster %llu\n",
					(unsigned long long)ci);
				goto compact_err;
			}
		} else {
			n = pread(in_fd, cluster_buf, cluster_size, phys_offset);
			if (n != (ssize_t)cluster_size) {
				fprintf(stderr, "Read error at cluster %llu\n",
					(unsigned long long)ci);
				goto compact_err;
			}
		}

		/* Skip zero clusters */
		if (is_all_zero(cluster_buf, cluster_size))
			continue;

		/* Allocate L2 table for new image if needed */
		if (new_l1[l1_idx] == 0) {
			new_l1[l1_idx] = new_alloc_offset;
			new_alloc_offset += cluster_size;
		}

		/* Recompress and write */
		int comp_len = LZ4_compress_default(
			(const char *)cluster_buf, (char *)comp_buf,
			cluster_size, comp_cap);

		if (comp_len > 0 &&
		    (uint32_t)comp_len < cluster_size - sizeof(uint32_t)) {
			uint32_t total_on_disk = ((sizeof(uint32_t) + comp_len) + 4095) & ~4095U;
			uint64_t phys = new_alloc_offset;
			uint32_t comp_size_be = htobe32_val(comp_len);

			if (pwrite(out_fd, &comp_size_be, sizeof(comp_size_be), phys) !=
			    sizeof(comp_size_be))
				goto compact_err;
			if (pwrite(out_fd, comp_buf, comp_len,
				   phys + sizeof(comp_size_be)) != comp_len)
				goto compact_err;

			new_l2[l1_idx][l2_idx] = LBD_QCOW2_L2_COMPRESSED | phys;
			new_alloc_offset += total_on_disk;
		} else {
			uint64_t phys = new_alloc_offset;

			if (pwrite(out_fd, cluster_buf, cluster_size, phys) !=
			    (ssize_t)cluster_size)
				goto compact_err;

			new_l2[l1_idx][l2_idx] = phys;
			new_alloc_offset += cluster_size;
		}
	}

	/* Write L2 tables */
	for (uint32_t i = 0; i < l1_size; i++) {
		if (new_l1[i] == 0)
			continue;

		uint64_t *disk_l2 = malloc(cluster_size);
		if (!disk_l2)
			goto compact_err;

		memset(disk_l2, 0, cluster_size);
		for (uint32_t j = 0; j < l2_entries; j++)
			disk_l2[j] = htobe64_val(new_l2[i][j]);

		/* Compute and store CRC32C trailer */
		{
			uint8_t *raw = (uint8_t *)disk_l2;
			uint32_t crc = crc32c_calc(raw, cluster_size - 4);
			_qcow2_put32(raw, cluster_size - 4, crc);
		}

		n = pwrite(out_fd, disk_l2, cluster_size, new_l1[i]);
		free(disk_l2);
		if (n != (ssize_t)cluster_size)
			goto compact_err;
	}

	/* Write header */
	memset(hdr, 0, LBD_QCOW2_HEADER_SIZE);
	lbd_qcow2_hdr_set_magic(hdr, LBD_QCOW2_MAGIC);
	lbd_qcow2_hdr_set_version(hdr, LBD_QCOW2_VERSION);
	lbd_qcow2_hdr_set_cluster_bits(hdr, cluster_bits);
	lbd_qcow2_hdr_set_virtual_size(hdr, virtual_size);
	lbd_qcow2_hdr_set_l1_table_offset(hdr, l1_table_offset);
	lbd_qcow2_hdr_set_l1_size(hdr, l1_size);
	lbd_qcow2_hdr_set_alloc_offset(hdr, new_alloc_offset);
	lbd_qcow2_hdr_set_comp_type(hdr, LBD_QCOW2_COMP_LZ4);
	lbd_qcow2_hdr_set_free_list(hdr, 0);

	if (pwrite(out_fd, hdr, LBD_QCOW2_HEADER_SIZE, 0) != LBD_QCOW2_HEADER_SIZE)
		goto compact_err;

	/* Write L1 table */
	{
		uint64_t *disk_l1 = calloc(l1_size, sizeof(uint64_t));
		if (!disk_l1)
			goto compact_err;
		for (uint32_t i = 0; i < l1_size; i++)
			disk_l1[i] = htobe64_val(new_l1[i]);
		n = pwrite(out_fd, disk_l1, l1_size * sizeof(uint64_t), l1_table_offset);
		free(disk_l1);
		if (n != (ssize_t)(l1_size * sizeof(uint64_t)))
			goto compact_err;
	}

	if (ftruncate(out_fd, new_alloc_offset) < 0)
		fprintf(stderr, "Warning: ftruncate failed\n");

	close(in_fd);
	close(out_fd);

	/* Replace original with compacted */
	if (rename(tmp_path, path) < 0) {
		fprintf(stderr, "Failed to rename %s -> %s: %s\n",
			tmp_path, path, strerror(errno));
		return 1;
	}

	printf("Compacted %s\n", path);
	{
		struct stat st;
		if (stat(path, &st) == 0) {
			printf("  File size: %llu bytes (%s)\n",
			       (unsigned long long)st.st_size,
			       fmt_size(st.st_size, (char[32]){0}, 32));
		}
	}

	/* Cleanup */
	for (uint32_t i = 0; i < l1_size; i++) {
		free(l2_tables[i]);
		free(new_l2[i]);
	}
	free(l2_tables);
	free(new_l2);
	free(l1_table);
	free(new_l1);
	free(cluster_buf);
	free(comp_buf);
	return 0;

compact_err:
	fprintf(stderr, "Compaction failed\n");
	close(in_fd);
	close(out_fd);
	unlink(tmp_path);
	for (uint32_t i = 0; i < l1_size; i++) {
		free(l2_tables[i]);
		free(new_l2[i]);
	}
	free(l2_tables);
	free(new_l2);
	free(l1_table);
	free(new_l1);
	free(cluster_buf);
	free(comp_buf);
	return 1;
}

/* ----------------------------------------------------------------
 * Watch command (log rotation notifications)
 * ---------------------------------------------------------------- */

static volatile sig_atomic_t watch_running = 1;

static void watch_sigint(int sig)
{
	(void)sig;
	watch_running = 0;
}

/*
 * Minimal CBOR encoder for userspace (write path).
 * Encodes a CBOR head: major type (top 3 bits) + value.
 */
static size_t cbor_write_head(uint8_t *buf, uint8_t major, uint64_t val)
{
	uint8_t mt = major << 5;

	if (val < 24) {
		buf[0] = mt | (uint8_t)val;
		return 1;
	} else if (val <= 0xFF) {
		buf[0] = mt | 24;
		buf[1] = (uint8_t)val;
		return 2;
	} else if (val <= 0xFFFF) {
		buf[0] = mt | 25;
		buf[1] = (uint8_t)(val >> 8);
		buf[2] = (uint8_t)val;
		return 3;
	} else if (val <= 0xFFFFFFFF) {
		buf[0] = mt | 26;
		buf[1] = (uint8_t)(val >> 24);
		buf[2] = (uint8_t)(val >> 16);
		buf[3] = (uint8_t)(val >> 8);
		buf[4] = (uint8_t)val;
		return 5;
	} else {
		buf[0] = mt | 27;
		buf[1] = (uint8_t)(val >> 56);
		buf[2] = (uint8_t)(val >> 48);
		buf[3] = (uint8_t)(val >> 40);
		buf[4] = (uint8_t)(val >> 32);
		buf[5] = (uint8_t)(val >> 24);
		buf[6] = (uint8_t)(val >> 16);
		buf[7] = (uint8_t)(val >> 8);
		buf[8] = (uint8_t)val;
		return 9;
	}
}

/*
 * Encode and write a watch command to the control device fd.
 * dev_index < 0 means watch all devices.
 */
static int cbor_write_watch_cmd(int fd, int dev_index)
{
	uint8_t buf[64];
	size_t pos = 0;
	const char *cmd_str = "watch";
	size_t cmd_len = 5;
	int map_items = (dev_index >= 0) ? 2 : 1;
	ssize_t n;

	/* map(1 or 2) */
	pos += cbor_write_head(buf + pos, 5, map_items);

	/* key 1: "watch" */
	pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_CMD);
	pos += cbor_write_head(buf + pos, 3, cmd_len);
	memcpy(buf + pos, cmd_str, cmd_len);
	pos += cmd_len;

	/* key 2: dev_index (optional) */
	if (dev_index >= 0) {
		pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_DEV);
		pos += cbor_write_head(buf + pos, 0, (uint64_t)dev_index);
	}

	n = write(fd, buf, pos);
	if (n < 0) {
		fprintf(stderr, "Failed to write watch command: %s\n",
			strerror(errno));
		return -1;
	}
	if ((size_t)n != pos) {
		fprintf(stderr, "Short write on watch command\n");
		return -1;
	}
	return 0;
}

/*
 * Decode a CBOR-encoded event message from a buffer.
 * Uses the existing fd-based cbor_read_* functions via a temporary
 * approach: we decode inline from the buffer.
 */
static int decode_watch_event(const uint8_t *buf, size_t len,
			      int *dev_index, char *label, size_t label_cap,
			      char *dir, size_t dir_cap,
			      uint64_t *log_seq, uint64_t *device_size)
{
	size_t pos = 0;
	uint8_t ib, major, ai;
	uint64_t map_count, key, val;
	uint64_t slen;

	/* Inline buffer-based CBOR decoder (mirrors cbor_dec.h logic) */
#define BUF_HEAD(major_out, val_out) do {			\
	if (pos >= len) return -1;				\
	ib = buf[pos++];					\
	*(major_out) = ib >> 5;					\
	ai = ib & 0x1F;					\
	if (ai < 24) { *(val_out) = ai; }			\
	else if (ai == 24) {					\
		if (pos + 1 > len) return -1;			\
		*(val_out) = buf[pos++];			\
	} else if (ai == 25) {					\
		if (pos + 2 > len) return -1;			\
		*(val_out) = ((uint64_t)buf[pos] << 8) | buf[pos+1]; \
		pos += 2;					\
	} else if (ai == 26) {					\
		if (pos + 4 > len) return -1;			\
		*(val_out) = ((uint64_t)buf[pos] << 24) |	\
			     ((uint64_t)buf[pos+1] << 16) |	\
			     ((uint64_t)buf[pos+2] << 8) |	\
			     buf[pos+3];			\
		pos += 4;					\
	} else if (ai == 27) {					\
		if (pos + 8 > len) return -1;			\
		*(val_out) = ((uint64_t)buf[pos] << 56) |	\
			     ((uint64_t)buf[pos+1] << 48) |	\
			     ((uint64_t)buf[pos+2] << 40) |	\
			     ((uint64_t)buf[pos+3] << 32) |	\
			     ((uint64_t)buf[pos+4] << 24) |	\
			     ((uint64_t)buf[pos+5] << 16) |	\
			     ((uint64_t)buf[pos+6] << 8) |	\
			     buf[pos+7];			\
		pos += 8;					\
	} else { return -1; }					\
} while (0)

	/* Read map header */
	BUF_HEAD(&major, &map_count);
	if (major != 5)
		return -1;

	*dev_index = -1;
	*log_seq = 0;
	*device_size = 0;
	label[0] = '\0';
	dir[0] = '\0';

	for (uint64_t i = 0; i < map_count; i++) {
		/* Read key (uint) */
		BUF_HEAD(&major, &key);
		if (major != 0)
			return -1;

		switch (key) {
		case LBD_EVENT_KEY_TYPE:
			/* text string — skip it */
			BUF_HEAD(&major, &slen);
			if (major != 3 || pos + slen > len)
				return -1;
			pos += slen;
			break;
		case LBD_EVENT_KEY_DEV:
			BUF_HEAD(&major, &val);
			if (major != 0)
				return -1;
			*dev_index = (int)val;
			break;
		case LBD_EVENT_KEY_LABEL:
			BUF_HEAD(&major, &slen);
			if (major != 3 || slen >= label_cap || pos + slen > len)
				return -1;
			memcpy(label, buf + pos, slen);
			label[slen] = '\0';
			pos += slen;
			break;
		case LBD_EVENT_KEY_DIR:
			BUF_HEAD(&major, &slen);
			if (major != 3 || slen >= dir_cap || pos + slen > len)
				return -1;
			memcpy(dir, buf + pos, slen);
			dir[slen] = '\0';
			pos += slen;
			break;
		case LBD_EVENT_KEY_SEQ:
			BUF_HEAD(&major, &val);
			if (major != 0)
				return -1;
			*log_seq = val;
			break;
		case LBD_EVENT_KEY_SIZE:
			BUF_HEAD(&major, &val);
			if (major != 0)
				return -1;
			*device_size = val;
			break;
		default:
			return -1;
		}
	}

#undef BUF_HEAD
	return 0;
}

static int cmd_watch(int argc, char **argv)
{
	int fd;
	int dev_filter = -1;
	int json = 0;
	struct pollfd pfd;
	struct sigaction sa;
	uint8_t rbuf[512];

	for (int i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--dev") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--dev requires a value\n");
				return 1;
			}
			dev_filter = atoi(argv[i]);
		} else if (strcmp(argv[i], "--json") == 0) {
			json = 1;
		} else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	fd = open(LBD_CTL_PATH, O_RDWR);
	if (fd < 0) {
		fprintf(stderr, "Cannot open %s: %s\n",
			LBD_CTL_PATH, strerror(errno));
		if (errno == ENOENT)
			fprintf(stderr, "Is the lbd module loaded?\n");
		return 1;
	}

	/* Send watch command */
	if (cbor_write_watch_cmd(fd, dev_filter) < 0) {
		close(fd);
		return 1;
	}

	if (!json) {
		if (dev_filter >= 0)
			fprintf(stderr, "Watching lbd%d for log rotations...\n",
				dev_filter);
		else
			fprintf(stderr, "Watching all devices for log rotations...\n");
	}

	/* Setup SIGINT handler for clean exit */
	memset(&sa, 0, sizeof(sa));
	sa.sa_handler = watch_sigint;
	sigemptyset(&sa.sa_mask);
	sa.sa_flags = 0;
	sigaction(SIGINT, &sa, NULL);
	sigaction(SIGTERM, &sa, NULL);

	pfd.fd = fd;
	pfd.events = POLLIN;

	while (watch_running) {
		int ret = poll(&pfd, 1, 1000);

		if (ret < 0) {
			if (errno == EINTR)
				continue;
			fprintf(stderr, "poll error: %s\n", strerror(errno));
			break;
		}
		if (ret == 0)
			continue;

		if (pfd.revents & POLLIN) {
			ssize_t n = read(fd, rbuf, sizeof(rbuf));

			if (n < 0) {
				if (errno == EINTR)
					continue;
				fprintf(stderr, "read error: %s\n",
					strerror(errno));
				break;
			}
			if (n == 0)
				break;

			int dev_idx;
			char label[32];
			char dir[LBD_LOG_PATH_MAX];
			uint64_t log_seq, device_size;

			if (decode_watch_event(rbuf, (size_t)n,
					       &dev_idx, label, sizeof(label),
					       dir, sizeof(dir),
					       &log_seq, &device_size) < 0) {
				fprintf(stderr, "Failed to decode event\n");
				continue;
			}

			if (json) {
				printf("{\"type\":\"log_rotated\","
				       "\"dev\":%d,"
				       "\"segment_label\":\"%s\","
				       "\"log_dir\":",
				       dev_idx, label);
				json_print_string(stdout, dir);
				printf(",\"log_seq\":%llu,"
				       "\"device_size\":%llu}\n",
				       (unsigned long long)log_seq,
				       (unsigned long long)device_size);
			} else {
				printf("lbd%d: %s/disk.%s.log (seq=%llu)\n",
				       dev_idx, dir, label,
				       (unsigned long long)log_seq);
			}
			fflush(stdout);
		}

		if (pfd.revents & (POLLERR | POLLHUP))
			break;
	}

	close(fd);
	return 0;
}

/* ----------------------------------------------------------------
 * Miss handler command
 * ---------------------------------------------------------------- */

/*
 * Encode and write a manage_misses command.
 */
static int cbor_write_manage_misses_cmd(int fd, int dev_index)
{
	uint8_t buf[64];
	size_t pos = 0;
	const char *cmd_str = "manage_misses";
	size_t cmd_len = strlen(cmd_str);
	ssize_t n;

	/* map(2) */
	pos += cbor_write_head(buf + pos, 5, 2);

	/* key 1: "manage_misses" */
	pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_CMD);
	pos += cbor_write_head(buf + pos, 3, cmd_len);
	memcpy(buf + pos, cmd_str, cmd_len);
	pos += cmd_len;

	/* key 2: dev_index */
	pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_DEV);
	pos += cbor_write_head(buf + pos, 0, (uint64_t)dev_index);

	n = write(fd, buf, pos);
	if (n < 0) {
		fprintf(stderr, "Failed to write manage_misses command: %s\n",
			strerror(errno));
		return -1;
	}
	if ((size_t)n != pos) {
		fprintf(stderr, "Short write on manage_misses command\n");
		return -1;
	}
	return 0;
}

/*
 * Encode and write a continue/retry command.
 */
static int cbor_write_miss_response(int fd, const char *action)
{
	uint8_t buf[64];
	size_t pos = 0;
	size_t cmd_len = strlen(action);
	ssize_t n;

	/* map(1) */
	pos += cbor_write_head(buf + pos, 5, 1);

	/* key 1: action */
	pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_CMD);
	pos += cbor_write_head(buf + pos, 3, cmd_len);
	memcpy(buf + pos, action, cmd_len);
	pos += cmd_len;

	n = write(fd, buf, pos);
	if (n < 0) {
		fprintf(stderr, "Failed to write %s command: %s\n",
			action, strerror(errno));
		return -1;
	}
	if ((size_t)n != pos) {
		fprintf(stderr, "Short write on %s command\n", action);
		return -1;
	}
	return 0;
}

/*
 * Decode a miss event from a CBOR buffer.
 * Returns 0 on success, -1 on error.
 */
static int decode_miss_event(const uint8_t *buf, size_t len,
			     int *dev_index, uint64_t *cluster)
{
	size_t pos = 0;
	uint8_t ib, major, ai;
	uint64_t map_count, key, val;
	uint64_t slen;

#define BUF_HEAD2(major_out, val_out) do {			\
	if (pos >= len) return -1;				\
	ib = buf[pos++];					\
	*(major_out) = ib >> 5;					\
	ai = ib & 0x1F;					\
	if (ai < 24) { *(val_out) = ai; }			\
	else if (ai == 24) {					\
		if (pos + 1 > len) return -1;			\
		*(val_out) = buf[pos++];			\
	} else if (ai == 25) {					\
		if (pos + 2 > len) return -1;			\
		*(val_out) = ((uint64_t)buf[pos] << 8) | buf[pos+1]; \
		pos += 2;					\
	} else if (ai == 26) {					\
		if (pos + 4 > len) return -1;			\
		*(val_out) = ((uint64_t)buf[pos] << 24) |	\
			     ((uint64_t)buf[pos+1] << 16) |	\
			     ((uint64_t)buf[pos+2] << 8) |	\
			     buf[pos+3];			\
		pos += 4;					\
	} else if (ai == 27) {					\
		if (pos + 8 > len) return -1;			\
		*(val_out) = ((uint64_t)buf[pos] << 56) |	\
			     ((uint64_t)buf[pos+1] << 48) |	\
			     ((uint64_t)buf[pos+2] << 40) |	\
			     ((uint64_t)buf[pos+3] << 32) |	\
			     ((uint64_t)buf[pos+4] << 24) |	\
			     ((uint64_t)buf[pos+5] << 16) |	\
			     ((uint64_t)buf[pos+6] << 8) |	\
			     buf[pos+7];			\
		pos += 8;					\
	} else { return -1; }					\
} while (0)

	BUF_HEAD2(&major, &map_count);
	if (major != 5)
		return -1;

	*dev_index = -1;
	*cluster = 0;

	for (uint64_t i = 0; i < map_count; i++) {
		BUF_HEAD2(&major, &key);
		if (major != 0)
			return -1;

		switch (key) {
		case LBD_MISS_KEY_TYPE:
			BUF_HEAD2(&major, &slen);
			if (major != 3 || pos + slen > len)
				return -1;
			pos += slen;
			break;
		case LBD_MISS_KEY_DEV:
			BUF_HEAD2(&major, &val);
			if (major != 0)
				return -1;
			*dev_index = (int)val;
			break;
		case LBD_MISS_KEY_CLUSTER:
			BUF_HEAD2(&major, &val);
			if (major != 0)
				return -1;
			*cluster = val;
			break;
		default:
			return -1;
		}
	}

#undef BUF_HEAD2
	return 0;
}

static int cmd_miss_handler(int argc, char **argv)
{
	int fd;
	int dev_index = -1;
	int json = 0;
	struct pollfd pfd;
	struct sigaction sa;
	uint8_t rbuf[512];

	for (int i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--dev") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--dev requires a value\n");
				return 1;
			}
			dev_index = atoi(argv[i]);
		} else if (strcmp(argv[i], "--json") == 0) {
			json = 1;
		} else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	if (dev_index < 0) {
		fprintf(stderr, "miss-handler requires --dev N\n");
		return 1;
	}

	fd = open(LBD_CTL_PATH, O_RDWR);
	if (fd < 0) {
		fprintf(stderr, "Cannot open %s: %s\n",
			LBD_CTL_PATH, strerror(errno));
		if (errno == ENOENT)
			fprintf(stderr, "Is the lbd module loaded?\n");
		return 1;
	}

	if (cbor_write_manage_misses_cmd(fd, dev_index) < 0) {
		close(fd);
		return 1;
	}

	if (!json)
		fprintf(stderr, "Handling block misses for lbd%d...\n",
			dev_index);

	memset(&sa, 0, sizeof(sa));
	sa.sa_handler = watch_sigint;
	sigemptyset(&sa.sa_mask);
	sa.sa_flags = 0;
	sigaction(SIGINT, &sa, NULL);
	sigaction(SIGTERM, &sa, NULL);

	pfd.fd = fd;
	pfd.events = POLLIN;

	while (watch_running) {
		int ret = poll(&pfd, 1, 1000);

		if (ret < 0) {
			if (errno == EINTR)
				continue;
			fprintf(stderr, "poll error: %s\n", strerror(errno));
			break;
		}
		if (ret == 0)
			continue;

		if (pfd.revents & POLLIN) {
			ssize_t n = read(fd, rbuf, sizeof(rbuf));

			if (n < 0) {
				if (errno == EINTR)
					continue;
				fprintf(stderr, "read error: %s\n",
					strerror(errno));
				break;
			}
			if (n == 0)
				break;

			int miss_dev;
			uint64_t miss_cluster;

			if (decode_miss_event(rbuf, (size_t)n,
					      &miss_dev, &miss_cluster) < 0) {
				fprintf(stderr, "Failed to decode miss event\n");
				continue;
			}

			if (json) {
				printf("{\"type\":\"block_miss\","
				       "\"dev\":%d,"
				       "\"cluster\":%llu}\n",
				       miss_dev,
				       (unsigned long long)miss_cluster);
			} else {
				printf("lbd%d: block miss at cluster %llu\n",
				       miss_dev,
				       (unsigned long long)miss_cluster);
			}
			fflush(stdout);

			/* Respond with continue */
			if (cbor_write_miss_response(fd, "continue") < 0)
				break;
		}

		if (pfd.revents & (POLLERR | POLLHUP))
			break;
	}

	close(fd);
	return 0;
}

/* ----------------------------------------------------------------
 * Swap command
 * ---------------------------------------------------------------- */

/*
 * Encode and write a swap command with a path.
 */
static int cbor_write_swap_cmd(int fd, const char *path)
{
	uint8_t buf[512];
	size_t pos = 0;
	const char *cmd_str = "swap";
	size_t cmd_len = strlen(cmd_str);
	size_t path_len = strlen(path);
	ssize_t n;

	/* map(2) */
	pos += cbor_write_head(buf + pos, 5, 2);

	/* key 1: "swap" */
	pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_CMD);
	pos += cbor_write_head(buf + pos, 3, cmd_len);
	memcpy(buf + pos, cmd_str, cmd_len);
	pos += cmd_len;

	/* key 3: path */
	pos += cbor_write_head(buf + pos, 0, LBD_WATCH_KEY_PATH);
	pos += cbor_write_head(buf + pos, 3, path_len);
	if (pos + path_len > sizeof(buf)) {
		fprintf(stderr, "Path too long for CBOR buffer\n");
		return -1;
	}
	memcpy(buf + pos, path, path_len);
	pos += path_len;

	n = write(fd, buf, pos);
	if (n < 0) {
		fprintf(stderr, "Failed to write swap command: %s\n",
			strerror(errno));
		return -1;
	}
	if ((size_t)n != pos) {
		fprintf(stderr, "Short write on swap command\n");
		return -1;
	}
	return 0;
}

static int cmd_swap(int argc, char **argv)
{
	int fd;
	int dev_index = -1;
	const char *path = NULL;
	char resolved[PATH_MAX];
	int json = 0;

	for (int i = 0; i < argc; i++) {
		if (strcmp(argv[i], "--json") == 0) {
			json = 1;
		} else if (strcmp(argv[i], "--dev") == 0) {
			if (++i >= argc) {
				fprintf(stderr, "--dev requires a value\n");
				return 1;
			}
			dev_index = atoi(argv[i]);
		} else if (!path) {
			path = argv[i];
		} else {
			fprintf(stderr, "Unexpected argument: %s\n", argv[i]);
			return 1;
		}
	}

	if (dev_index < 0) {
		fprintf(stderr, "swap requires --dev N\n");
		return 1;
	}
	if (!path) {
		fprintf(stderr, "swap requires a path argument\n");
		return 1;
	}

	if (!realpath(path, resolved)) {
		fprintf(stderr, "Cannot resolve path '%s': %s\n",
			path, strerror(errno));
		return 1;
	}

	fd = open(LBD_CTL_PATH, O_RDWR);
	if (fd < 0) {
		fprintf(stderr, "Cannot open %s: %s\n",
			LBD_CTL_PATH, strerror(errno));
		if (errno == ENOENT)
			fprintf(stderr, "Is the lbd module loaded?\n");
		return 1;
	}

	/* First register as miss handler for the device */
	if (cbor_write_manage_misses_cmd(fd, dev_index) < 0) {
		close(fd);
		return 1;
	}

	/* Send swap command */
	if (cbor_write_swap_cmd(fd, resolved) < 0) {
		close(fd);
		return 1;
	}

	if (json) {
		printf("{\"device\": \"/dev/lbd%d\", \"index\": %d, \"base\": ",
		       dev_index, dev_index);
		json_print_string(stdout, resolved);
		printf("}\n");
	} else {
		printf("Swapped base layer for lbd%d to %s\n", dev_index, resolved);
	}
	close(fd);
	return 0;
}

/* ----------------------------------------------------------------
 * Main
 * ---------------------------------------------------------------- */

static void usage(void)
{
	fprintf(stderr,
		"Usage:\n"
		"  lbdctl add [opts] <path>           Create a new lbd device backed by <path>\n"
		"  lbdctl remove <N>                  Remove /dev/lbdN\n"
		"  lbdctl list                        List all active lbd devices\n"
		"  lbdctl watch [opts]                Watch for log rotation events\n"
		"  lbdctl miss-handler [opts]         Handle block miss events\n"
		"  lbdctl swap [opts] <path>          Swap base layer for a device\n"
		"  lbdctl log [opts] <logfile>        Read and display a log file\n"
		"  lbdctl create --size <S> <path>    Create empty qcow2-lz4 image\n"
		"  lbdctl convert <flat> <qcow2>      Convert flat image to qcow2-lz4\n"
		"  lbdctl extract <qcow2> <flat>      Extract qcow2-lz4 to flat image\n"
		"  lbdctl compact <qcow2>             Compact qcow2-lz4 image (reclaim space)\n"
		"\n"
		"Global options:\n"
		"  --json                   Output as JSON\n"
		"\n"
		"Add options:\n"
		"  --log-dir <dir>          Directory to write log files (required)\n"
		"  --base <path>            Read-only base layer image (thin snapshot)\n"
		"  --log-max-size <bytes>   Segment rotation size (default 64 MiB)\n"
		"  --log-max-age <secs>     Segment rotation age (default 60s)\n"
		"\n"
		"Watch options:\n"
		"  --dev <N>      Only watch device N (default: all)\n"
		"  --json         Output events as JSON\n"
		"\n"
		"Miss handler options:\n"
		"  --dev <N>      Device to handle misses for (required)\n"
		"  --json         Output events as JSON\n"
		"\n"
		"Swap options:\n"
		"  --dev <N>      Device to swap base for (required)\n"
		"\n"
		"Create options:\n"
		"  --size <bytes>           Virtual device size (supports K/M/G/T suffixes)\n"
		"  --cluster-bits <N>       log2(cluster_size), default 16 (64 KiB)\n"
		"\n"
		"Log options:\n"
		"  --json        Output as JSON\n"
		"  --data        Include hex data in output\n");
}

int main(int argc, char **argv)
{
	if (argc < 2) {
		usage();
		return 1;
	}

	if (strcmp(argv[1], "add") == 0) {
		if (argc < 3) {
			fprintf(stderr, "add requires a path argument\n");
			return 1;
		}
		return cmd_add(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "remove") == 0) {
		return cmd_remove(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "list") == 0) {
		return cmd_list(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "watch") == 0) {
		return cmd_watch(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "miss-handler") == 0) {
		return cmd_miss_handler(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "swap") == 0) {
		return cmd_swap(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "create") == 0) {
		if (argc < 3) {
			fprintf(stderr, "create requires --size and a path\n");
			return 1;
		}
		return cmd_create(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "convert") == 0) {
		if (argc < 4) {
			fprintf(stderr, "convert requires <flat-file> <qcow2-file>\n");
			return 1;
		}
		return cmd_convert(argv[2], argv[3]);
	}

	if (strcmp(argv[1], "extract") == 0) {
		if (argc < 4) {
			fprintf(stderr, "extract requires <qcow2-file> <flat-file>\n");
			return 1;
		}
		return cmd_extract(argv[2], argv[3]);
	}

	if (strcmp(argv[1], "compact") == 0) {
		if (argc < 3) {
			fprintf(stderr, "compact requires a qcow2 file path\n");
			return 1;
		}
		return cmd_compact(argc - 2, argv + 2);
	}

	if (strcmp(argv[1], "log") == 0) {
		int json = 0, show_data = 0;
		const char *logpath = NULL;

		for (int i = 2; i < argc; i++) {
			if (strcmp(argv[i], "--json") == 0)
				json = 1;
			else if (strcmp(argv[i], "--data") == 0)
				show_data = 1;
			else if (!logpath)
				logpath = argv[i];
			else {
				fprintf(stderr, "Unexpected argument: %s\n",
					argv[i]);
				return 1;
			}
		}

		if (!logpath) {
			fprintf(stderr, "log requires a log file path\n");
			return 1;
		}
		return cmd_log(logpath, json, show_data);
	}

	fprintf(stderr, "Unknown command: %s\n", argv[1]);
	usage();
	return 1;
}
