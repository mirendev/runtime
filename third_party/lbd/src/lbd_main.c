// SPDX-License-Identifier: GPL-2.0
/*
 * LBD - Logging Block Device
 *
 * A block device backed by a file (like loop) that logs all write
 * operations to a companion .log file for change tracking / audit / replay.
 */

#include <linux/blkdev.h>
#include <linux/blk-mq.h>
#include <linux/crc32.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/highmem.h>
#include <linux/idr.h>
#include <linux/init.h>
#include <linux/kernel.h>
#include <linux/module.h>
#include <linux/sched.h>
#include <linux/slab.h>
#include <linux/ktime.h>
#include <linux/namei.h>
#include <linux/falloc.h>

#include "lbd.h"

#if LBD_HAS_MNT_IDMAP
#include <linux/mnt_idmapping.h>
#endif
#include "lbd_qcow2.h"
#include "cbor_enc.h"
#include "cbor_dec.h"
#include "lz4_kcompat.h"
#include <linux/poll.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("lbd authors");
MODULE_DESCRIPTION("Logging Block Device");
MODULE_VERSION(LBD_VERSION);

static int lbd_major;
static DEFINE_IDR(lbd_devices);
static DEFINE_MUTEX(lbd_devices_mutex);
static struct miscdevice lbd_misc;

/* ----------------------------------------------------------------
 * Log rotation watchers
 * ---------------------------------------------------------------- */

#define LBD_WATCHER_QUEUE_SIZE 64

struct lbd_watcher_event {
	int   dev_index;
	u64   log_seq;
	u64   device_size;
	char  segment_label[25];
	char  log_dir[LBD_LOG_PATH_MAX];
};

struct lbd_watcher {
	struct list_head        list;
	spinlock_t              lock;
	wait_queue_head_t       wq;
	struct lbd_watcher_event queue[LBD_WATCHER_QUEUE_SIZE];
	unsigned int            head, tail, count;
	int                     filter_dev; /* -1 = all */
};

static LIST_HEAD(lbd_watchers);
static DEFINE_SPINLOCK(lbd_watchers_lock);

#ifdef CONFIG_LBD_MISS_HANDLER
/* ----------------------------------------------------------------
 * Block miss handler (for remote-fetch workflow)
 * ---------------------------------------------------------------- */

struct lbd_miss_pending {
	struct completion       done;
	enum lbd_miss_action    action;
};

struct lbd_miss_handler {
	spinlock_t              lock;
	wait_queue_head_t       wq;
	int                     dev_index;
	struct lbd_miss_pending *pending;    /* NULL = idle */
	bool                    has_event;
	u64                     miss_cluster;
};
#endif /* CONFIG_LBD_MISS_HANDLER */

/* Control fd state — supports both watch and miss handler on same fd */
struct lbd_ctl_state {
	struct lbd_watcher      *watcher;   /* NULL until "watch" command */
#ifdef CONFIG_LBD_MISS_HANDLER
	struct lbd_miss_handler *miss;      /* NULL until "manage_misses" command */
#endif
};

/* ----------------------------------------------------------------
 * Block device operations
 * ---------------------------------------------------------------- */

#if LBD_HAS_GENDISK_OPEN
static int lbd_open(struct gendisk *disk, unsigned int mode)
{
	struct lbd_device *dev = disk->private_data;
#else
static int lbd_open(struct block_device *bdev, fmode_t mode)
{
	struct lbd_device *dev = bdev->bd_disk->private_data;
#endif

	if (dev->state != LBD_STATE_BOUND)
		return -ENXIO;
	atomic_inc(&dev->open_count);
	return 0;
}

#if LBD_HAS_GENDISK_OPEN
static void lbd_release(struct gendisk *disk)
{
	struct lbd_device *dev = disk->private_data;
#else
static void lbd_release(struct gendisk *disk, fmode_t mode)
{
	struct lbd_device *dev = disk->private_data;
#endif

	atomic_dec(&dev->open_count);
}

static const struct block_device_operations lbd_fops = {
	.owner		= THIS_MODULE,
	.open		= lbd_open,
	.release	= lbd_release,
};

/* ----------------------------------------------------------------
 * I/O: read path
 * ---------------------------------------------------------------- */

static int lbd_do_read(struct lbd_device *dev, struct request *rq)
{
	struct req_iterator iter;
	struct bio_vec bvec;
	loff_t pos = (loff_t)blk_rq_pos(rq) << SECTOR_SHIFT;
	ssize_t ret;

	if (dev->is_qcow2)
		return lbd_qcow2_read(dev, rq);

	rq_for_each_segment(bvec, rq, iter) {
		void *mapped = kmap_local_page(bvec.bv_page);

		ret = kernel_read(dev->backing_file, mapped + bvec.bv_offset,
				  bvec.bv_len, &pos);
		kunmap_local(mapped);

		if (ret != bvec.bv_len) {
			if (ret >= 0)
				ret = -EIO;
			return ret;
		}
	}

	atomic64_inc(&dev->stat_reads);
	atomic64_add(blk_rq_bytes(rq), &dev->stat_read_bytes);
	return 0;
}

/* Forward declarations */
static const struct attribute_group lbd_attr_group;

/* Forward declarations for log segmentation */
static int lbd_open_log_file(struct lbd_device *dev);
static void lbd_rotate_segment(struct lbd_device *dev);
static void lbd_maybe_rotate_segment(struct lbd_device *dev);
static void lbd_notify_watchers(struct lbd_device *dev);

/* ----------------------------------------------------------------
 * I/O: log write buffer
 * ---------------------------------------------------------------- */

static int lbd_log_flush(struct lbd_device *dev)
{
	loff_t pos;
	ssize_t ret;

	if (dev->log_buf_used == 0)
		return 0;

	/* Lazy-open: create the segment file on first flush */
	if (!dev->log_file) {
		int err = lbd_open_log_file(dev);
		if (err) {
			dev->log_buf_used = 0;
			return err;
		}
	}

	pos = i_size_read(file_inode(dev->log_file));
	ret = kernel_write(dev->log_file, dev->log_buf,
			   dev->log_buf_used, &pos);
	if (ret != dev->log_buf_used) {
		pr_warn_ratelimited("lbd%d: log flush failed (%zd)\n",
				    dev->index, ret);
		dev->log_buf_used = 0;
		return ret < 0 ? ret : -EIO;
	}
	dev->log_buf_used = 0;
	return 0;
}

static int lbd_log_buf_append(struct lbd_device *dev,
			      const void *data, size_t len)
{
	if (dev->log_buf_used + len > LBD_LOG_BUF_SIZE) {
		int ret = lbd_log_flush(dev);
		if (ret)
			return ret;
	}
	memcpy(dev->log_buf + dev->log_buf_used, data, len);
	dev->log_buf_used += len;
	return 0;
}

/* ----------------------------------------------------------------
 * I/O: log write
 * ---------------------------------------------------------------- */

static void lbd_log_write(struct lbd_device *dev, struct request *rq)
{
	struct req_iterator iter;
	struct bio_vec bvec;
	u32 data_len = blk_rq_bytes(rq);
	void *data_buf;
	void *comp_buf = NULL;
	int comp_len;
	int comp_cap;
	size_t offset = 0;
	u8 tmp[128];
	struct cbor_enc e;
	u32 crc;
	u64 ts, block;
	int ret;

	if (!data_len)
		return;

	data_buf = kvmalloc(data_len, GFP_NOIO);
	if (!data_buf) {
		pr_warn_ratelimited("lbd%d: log alloc failed\n", dev->index);
		return;
	}

	/* Gather write data from bio vecs */
	rq_for_each_segment(bvec, rq, iter) {
		void *mapped = kmap_local_page(bvec.bv_page);
		memcpy(data_buf + offset, mapped + bvec.bv_offset, bvec.bv_len);
		kunmap_local(mapped);
		offset += bvec.bv_len;
	}

	/* CRC on uncompressed data */
	crc = crc32(~0U, data_buf, data_len) ^ ~0U;

	/* LZ4-compress the data */
	comp_cap = LZ4_compressBound(data_len);
	comp_buf = kvmalloc(comp_cap, GFP_NOIO);
	if (!comp_buf) {
		pr_warn_ratelimited("lbd%d: log compress alloc failed\n",
				    dev->index);
		kvfree(data_buf);
		return;
	}

	comp_len = LZ4_compress_fast_extState(dev->lz4_state,
					      data_buf, comp_buf,
					      data_len, comp_cap, 1);
	if (comp_len <= 0) {
		pr_warn_ratelimited("lbd%d: LZ4 compression failed\n",
				    dev->index);
		kvfree(comp_buf);
		kvfree(data_buf);
		return;
	}

	ts = ktime_get_real_ns();
	block = (loff_t)blk_rq_pos(rq) * 512 / LBD_BLOCK_SIZE;

	mutex_lock(&dev->log_mutex);

	cbor_enc_init(&e, tmp, sizeof(tmp));

	cbor_enc_map(&e, 7);

	cbor_enc_uint(&e, LBD_CBOR_KEY_OP);
	cbor_enc_text(&e, "W", 1);

	cbor_enc_uint(&e, LBD_CBOR_KEY_TIMESTAMP);
	cbor_enc_uint(&e, ts);

	cbor_enc_uint(&e, LBD_CBOR_KEY_SEQUENCE);
	cbor_enc_uint(&e, dev->log_seq++);

	cbor_enc_uint(&e, LBD_CBOR_KEY_BLOCK);
	cbor_enc_uint(&e, block);

	/* Length = uncompressed size (needed for decompression) */
	cbor_enc_uint(&e, LBD_CBOR_KEY_LENGTH);
	cbor_enc_uint(&e, data_len);

	cbor_enc_uint(&e, LBD_CBOR_KEY_CHECKSUM);
	cbor_enc_uint(&e, crc);

	/* Data = LZ4-compressed bytes */
	cbor_enc_uint(&e, LBD_CBOR_KEY_DATA);
	cbor_enc_bytes_hdr(&e, comp_len);

	if (e.err) {
		dev->log_seq--;
		goto warn;
	}

	ret = lbd_log_buf_append(dev, tmp, cbor_enc_len(&e));
	if (ret)
		goto warn;

	ret = lbd_log_buf_append(dev, comp_buf, comp_len);
	if (ret)
		goto warn;

	dev->log_has_entries = true;
	lbd_maybe_rotate_segment(dev);
	mutex_unlock(&dev->log_mutex);
	kvfree(comp_buf);
	kvfree(data_buf);
	return;

warn:
	mutex_unlock(&dev->log_mutex);
	kvfree(comp_buf);
	kvfree(data_buf);
	pr_warn_ratelimited("lbd%d: log write failed (%d)\n", dev->index, ret);
}

/* ----------------------------------------------------------------
 * I/O: write path
 * ---------------------------------------------------------------- */

static int lbd_do_write(struct lbd_device *dev, struct request *rq)
{
	struct req_iterator iter;
	struct bio_vec bvec;
	loff_t pos = (loff_t)blk_rq_pos(rq) << SECTOR_SHIFT;
	ssize_t ret;

	if (dev->is_qcow2) {
		ret = lbd_qcow2_write(dev, rq);
		if (ret)
			return ret;
		lbd_log_write(dev, rq);
		atomic64_inc(&dev->stat_writes);
		atomic64_add(blk_rq_bytes(rq), &dev->stat_write_bytes);
		return 0;
	}

	rq_for_each_segment(bvec, rq, iter) {
		void *mapped = kmap_local_page(bvec.bv_page);

		ret = kernel_write(dev->backing_file, mapped + bvec.bv_offset,
				   bvec.bv_len, &pos);
		kunmap_local(mapped);

		if (ret != bvec.bv_len) {
			if (ret >= 0)
				ret = -EIO;
			return ret;
		}
	}

	/* Log the write - failure is non-fatal */
	lbd_log_write(dev, rq);

	atomic64_inc(&dev->stat_writes);
	atomic64_add(blk_rq_bytes(rq), &dev->stat_write_bytes);
	return 0;
}

/* ----------------------------------------------------------------
 * I/O: discard (TRIM) path
 * ---------------------------------------------------------------- */

static void lbd_log_discard(struct lbd_device *dev, struct request *rq)
{
	u8 tmp[64];
	struct cbor_enc e;
	u64 ts = ktime_get_real_ns();
	u64 block = (loff_t)blk_rq_pos(rq) * 512 / LBD_BLOCK_SIZE;
	u32 length = blk_rq_bytes(rq);
	int ret;

	mutex_lock(&dev->log_mutex);

	cbor_enc_init(&e, tmp, sizeof(tmp));

	cbor_enc_map(&e, 5);

	cbor_enc_uint(&e, LBD_CBOR_KEY_OP);
	cbor_enc_text(&e, "T", 1);

	cbor_enc_uint(&e, LBD_CBOR_KEY_TIMESTAMP);
	cbor_enc_uint(&e, ts);

	cbor_enc_uint(&e, LBD_CBOR_KEY_SEQUENCE);
	cbor_enc_uint(&e, dev->log_seq++);

	cbor_enc_uint(&e, LBD_CBOR_KEY_BLOCK);
	cbor_enc_uint(&e, block);

	cbor_enc_uint(&e, LBD_CBOR_KEY_LENGTH);
	cbor_enc_uint(&e, length);

	if (e.err) {
		dev->log_seq--;
		goto warn;
	}

	ret = lbd_log_buf_append(dev, tmp, cbor_enc_len(&e));
	if (ret)
		goto warn;

	dev->log_has_entries = true;
	lbd_maybe_rotate_segment(dev);
	mutex_unlock(&dev->log_mutex);
	return;

warn:
	mutex_unlock(&dev->log_mutex);
	pr_warn_ratelimited("lbd%d: log discard failed (%d)\n", dev->index, ret);
}

static int lbd_do_discard(struct lbd_device *dev, struct request *rq)
{
	loff_t pos = (loff_t)blk_rq_pos(rq) << SECTOR_SHIFT;
	unsigned int len = blk_rq_bytes(rq);
	int ret;

	if (dev->is_qcow2) {
		ret = lbd_qcow2_discard(dev, rq);
		if (ret)
			return ret;
		lbd_log_discard(dev, rq);
		atomic64_inc(&dev->stat_trims);
		atomic64_add(len, &dev->stat_trim_bytes);
		return 0;
	}

	if (dev->backing_file->f_op->fallocate) {
		ret = dev->backing_file->f_op->fallocate(dev->backing_file,
			FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE, pos, len);
		if (ret && ret != -EINVAL && ret != -EOPNOTSUPP)
			return ret;
	}

	lbd_log_discard(dev, rq);

	atomic64_inc(&dev->stat_trims);
	atomic64_add(len, &dev->stat_trim_bytes);
	return 0;
}

/* ----------------------------------------------------------------
 * I/O: request dispatch
 * ---------------------------------------------------------------- */

static void lbd_handle_request(struct lbd_device *dev, struct request *rq)
{
	struct lbd_cmd *cmd = blk_mq_rq_to_pdu(rq);
	int ret;

	switch (req_op(rq)) {
	case REQ_OP_READ:
		ret = lbd_do_read(dev, rq);
		break;
	case REQ_OP_WRITE:
		ret = lbd_do_write(dev, rq);
		break;
	case REQ_OP_DISCARD:
		ret = lbd_do_discard(dev, rq);
		break;
	case REQ_OP_FLUSH:
		ret = vfs_fsync(dev->backing_file, 0);
		if (!ret) {
			mutex_lock(&dev->log_mutex);
			if (dev->log_has_entries)
				lbd_rotate_segment(dev);
			mutex_unlock(&dev->log_mutex);
		}
		break;
	default:
		ret = -EIO;
		break;
	}

	cmd->ret = ret;
}

static void lbd_work_fn(struct work_struct *work)
{
	struct lbd_device *dev = container_of(work, struct lbd_device, work);
	struct lbd_cmd *cmd;
	LIST_HEAD(local_list);
	unsigned int saved_flags = current->flags;

	/* Prevent writeback deadlock (same technique as loop driver) */
	current->flags |= PF_LOCAL_THROTTLE | PF_MEMALLOC_NOIO;

	spin_lock_irq(&dev->cmd_lock);
	list_splice_init(&dev->cmd_list, &local_list);
	spin_unlock_irq(&dev->cmd_lock);

	while (!list_empty(&local_list)) {
		cmd = list_first_entry(&local_list, struct lbd_cmd, list_entry);
		list_del(&cmd->list_entry);

		lbd_handle_request(dev,
			blk_mq_rq_from_pdu(cmd));

		blk_mq_complete_request(blk_mq_rq_from_pdu(cmd));
	}

	current->flags = (current->flags & ~(PF_LOCAL_THROTTLE | PF_MEMALLOC_NOIO)) |
			 (saved_flags & (PF_LOCAL_THROTTLE | PF_MEMALLOC_NOIO));
}

static void lbd_complete_rq(struct request *rq)
{
	struct lbd_cmd *cmd = blk_mq_rq_to_pdu(rq);

	blk_mq_end_request(rq, cmd->ret ? BLK_STS_IOERR : BLK_STS_OK);
}

static blk_status_t lbd_queue_rq(struct blk_mq_hw_ctx *hctx,
				 const struct blk_mq_queue_data *bd)
{
	struct lbd_device *dev = hctx->queue->queuedata;
	struct request *rq = bd->rq;
	struct lbd_cmd *cmd = blk_mq_rq_to_pdu(rq);

	blk_mq_start_request(rq);

	if (dev->state != LBD_STATE_BOUND)
		return BLK_STS_IOERR;

	INIT_LIST_HEAD(&cmd->list_entry);
	cmd->ret = 0;

	spin_lock_irq(&dev->cmd_lock);
	list_add_tail(&cmd->list_entry, &dev->cmd_list);
	spin_unlock_irq(&dev->cmd_lock);

	queue_work(dev->wq, &dev->work);
	return BLK_STS_OK;
}

static const struct blk_mq_ops lbd_mq_ops = {
	.queue_rq	= lbd_queue_rq,
	.complete	= lbd_complete_rq,
};

/* ----------------------------------------------------------------
 * Device lifecycle
 * ---------------------------------------------------------------- */

static int lbd_write_log_header(struct lbd_device *dev)
{
	u8 tmp[512];
	struct cbor_enc e;
	size_t path_len = strlen(dev->backing_path);

	cbor_enc_init(&e, tmp, sizeof(tmp));

	cbor_enc_map(&e, 5);

	cbor_enc_uint(&e, LBD_CBOR_KEY_HDR_VERSION);
	cbor_enc_uint(&e, LBD_LOG_VERSION);

	cbor_enc_uint(&e, LBD_CBOR_KEY_HDR_BLOCK_SIZE);
	cbor_enc_uint(&e, LBD_BLOCK_SIZE);

	cbor_enc_uint(&e, LBD_CBOR_KEY_HDR_SEGMENT_LABEL);
	cbor_enc_text(&e, dev->log_segment_label,
		      strlen(dev->log_segment_label));

	cbor_enc_uint(&e, LBD_CBOR_KEY_HDR_DEVICE_SIZE);
	cbor_enc_uint(&e, dev->size);

	cbor_enc_uint(&e, LBD_CBOR_KEY_HDR_BACKING_PATH);
	cbor_enc_text(&e, dev->backing_path, path_len);

	if (e.err)
		return e.err;

	return lbd_log_buf_append(dev, tmp, cbor_enc_len(&e));
}

/* ----------------------------------------------------------------
 * Log segmentation
 * ---------------------------------------------------------------- */

static int lbd_log_name_tmp(const char *label, char *buf, size_t sz)
{
	return snprintf(buf, sz, "disk.%s.log.tmp", label);
}

static int lbd_log_name_final(const char *label, char *buf, size_t sz)
{
	return snprintf(buf, sz, "disk.%s.log", label);
}

/*
 * Generate a TAI64N label from the current wall-clock time.
 * Format: 16 hex digits (TAI seconds) + 8 hex digits (nanoseconds).
 * buf must be at least 25 bytes (24 chars + NUL).
 */
static void lbd_tai64n_label(char *buf)
{
	struct timespec64 ts;
	u64 tai_secs;

	ktime_get_real_ts64(&ts);
	tai_secs = (u64)ts.tv_sec + 0x4000000000000000ULL;
	snprintf(buf, 25, "%016llx%08lx", tai_secs, ts.tv_nsec);
}

static int lbd_rename_file(struct file *old_file, const char *new_basename)
{
	struct dentry *old_dentry = old_file->f_path.dentry;
	struct dentry *parent = old_dentry->d_parent;
	struct dentry *new_dentry;
	struct renamedata rd;
	int ret;

	lock_rename(parent, parent);

#if LBD_HAS_LOOKUP_ONE_QSTR
	{
		struct qstr qname = QSTR_INIT(new_basename,
					      strlen(new_basename));
		new_dentry = lookup_one(&nop_mnt_idmap, &qname, parent);
	}
#elif LBD_HAS_RENAME_NO_DIR
	new_dentry = lookup_one(&nop_mnt_idmap, new_basename, parent,
				strlen(new_basename));
#else
	new_dentry = lookup_one_len(new_basename, parent, strlen(new_basename));
#endif
	if (IS_ERR(new_dentry)) {
		ret = PTR_ERR(new_dentry);
		goto out_unlock;
	}

	memset(&rd, 0, sizeof(rd));
#if LBD_HAS_RENAME_PARENT
	rd.mnt_idmap = &nop_mnt_idmap;
	rd.old_parent = parent;
	rd.old_dentry = old_dentry;
	rd.new_parent = parent;
	rd.new_dentry = new_dentry;
#elif LBD_HAS_RENAME_SINGLE_IDMAP
	rd.mnt_idmap = &nop_mnt_idmap;
	rd.old_dentry = old_dentry;
	rd.new_dentry = new_dentry;
#elif LBD_HAS_RENAME_NO_DIR
	rd.old_mnt_idmap = &nop_mnt_idmap;
	rd.old_dentry = old_dentry;
	rd.new_mnt_idmap = &nop_mnt_idmap;
	rd.new_dentry = new_dentry;
#elif LBD_HAS_MNT_IDMAP
	rd.old_mnt_idmap = &nop_mnt_idmap;
	rd.old_dir = d_inode(parent);
	rd.old_dentry = old_dentry;
	rd.new_mnt_idmap = &nop_mnt_idmap;
	rd.new_dir = d_inode(parent);
	rd.new_dentry = new_dentry;
#else
	rd.old_mnt_userns = &init_user_ns;
	rd.old_dir = d_inode(parent);
	rd.old_dentry = old_dentry;
	rd.new_mnt_userns = &init_user_ns;
	rd.new_dir = d_inode(parent);
	rd.new_dentry = new_dentry;
#endif

	ret = vfs_rename(&rd);
	dput(new_dentry);

out_unlock:
	unlock_rename(parent, parent);
	return ret;
}

/*
 * Lazy-open the .log.tmp file for the current segment.  Called from
 * lbd_log_flush() when buffered data first needs to hit disk.
 */
static int lbd_open_log_file(struct lbd_device *dev)
{
	char name[48];
	struct file *f;

	lbd_log_name_tmp(dev->log_segment_label, name, sizeof(name));

	/*
	 * Open relative to the log directory rather than using an
	 * absolute path.  This avoids path resolution failures when
	 * called from a kworker whose mount namespace differs from
	 * the process that created the device.
	 */
	f = file_open_root(&dev->log_dir_path, name,
			   O_RDWR | O_CREAT | O_TRUNC | O_LARGEFILE, 0600);
	if (IS_ERR(f)) {
		pr_warn("lbd%d: cannot open %s: %ld\n",
			dev->index, name, PTR_ERR(f));
		return PTR_ERR(f);
	}

	dev->log_file = f;
	return 0;
}

/*
 * Begin a new log segment.  Resets the buffer, generates a TAI64N
 * label, and writes the CBOR header into the buffer.  No file is
 * created on disk — that happens lazily in lbd_log_flush().
 */
static void lbd_begin_segment(struct lbd_device *dev)
{
	dev->log_file = NULL;
	dev->log_buf_used = 0;
	dev->log_has_entries = false;
	dev->segment_start_time = ktime_get();
	lbd_tai64n_label(dev->log_segment_label);
	lbd_write_log_header(dev);
}

static void lbd_rotate_segment(struct lbd_device *dev)
{
	char final_name[48];
	int ret;

	if (!dev->log_has_entries)
		return;

	/* Flush buffer (lazy-opens .log.tmp) then fsync */
	lbd_log_flush(dev);
	if (!dev->log_file)
		goto next;

	vfs_fsync(dev->log_file, 0);

	/* Rename disk.<tai64n>.log.tmp -> disk.<tai64n>.log */
	lbd_log_name_final(dev->log_segment_label, final_name,
			   sizeof(final_name));

	ret = lbd_rename_file(dev->log_file, final_name);
	if (ret)
		pr_warn("lbd%d: rename to %s failed: %d\n",
			dev->index, final_name, ret);

	fput(dev->log_file);
	dev->log_file = NULL;

next:
	atomic64_inc(&dev->stat_log_rotations);

	/* Notify watchers before lbd_begin_segment() overwrites the label */
	lbd_notify_watchers(dev);

	/* Begin fresh segment (buffer only, no file yet) */
	lbd_begin_segment(dev);

	/* Reschedule age timer */
	if (dev->log_max_age_secs > 0)
		mod_delayed_work(system_wq, &dev->log_rotate_dwork,
				 msecs_to_jiffies(dev->log_max_age_secs * 1000));
}

static void lbd_log_rotate_work_fn(struct work_struct *work)
{
	struct lbd_device *dev = container_of(work, struct lbd_device,
					      log_rotate_dwork.work);

	mutex_lock(&dev->log_mutex);
	if (dev->log_has_entries)
		lbd_rotate_segment(dev);
	mutex_unlock(&dev->log_mutex);
}

static void lbd_maybe_rotate_segment(struct lbd_device *dev)
{
	loff_t file_size = dev->log_file ?
		i_size_read(file_inode(dev->log_file)) : 0;

	if (file_size + dev->log_buf_used >= (loff_t)dev->log_max_size)
		lbd_rotate_segment(dev);
}

static void lbd_finalize_log(struct lbd_device *dev)
{
	char final_name[48];

	if (dev->log_has_entries)
		lbd_log_flush(dev);

	if (!dev->log_file)
		return;

	vfs_fsync(dev->log_file, 0);

	if (dev->log_has_entries) {
		lbd_log_name_final(dev->log_segment_label, final_name,
				   sizeof(final_name));
		lbd_rename_file(dev->log_file, final_name);
		lbd_notify_watchers(dev);
	}

	fput(dev->log_file);
	dev->log_file = NULL;
}

static void lbd_destroy_device(struct lbd_device *dev)
{
	dev->state = LBD_STATE_REMOVING;

	cancel_delayed_work_sync(&dev->log_rotate_dwork);

	device_remove_group(disk_to_dev(dev->gd), &lbd_attr_group);
	del_gendisk(dev->gd);
	flush_workqueue(dev->wq);
	destroy_workqueue(dev->wq);

	lbd_finalize_log(dev);

	if (dev->base)
		lbd_qcow2_base_destroy(dev->base);

	if (dev->is_qcow2)
		lbd_qcow2_destroy(dev);

	kvfree(dev->lz4_state);
	kvfree(dev->log_buf);
	path_put(&dev->log_dir_path);
	if (dev->backing_file) {
		vfs_fsync(dev->backing_file, 0);
		fput(dev->backing_file);
	}

	put_disk(dev->gd);
	blk_mq_free_tag_set(&dev->tag_set);
	kfree(dev);
}

static int lbd_add_device(struct lbd_ctl_add __user *uarg)
{
	struct lbd_ctl_add arg;
	struct lbd_device *dev;
	struct inode *inode;
	int ret, idx;

	if (copy_from_user(&arg, uarg, sizeof(arg)))
		return -EFAULT;

	arg.path[LBD_LOG_PATH_MAX - 1] = '\0';
	arg.log_dir[LBD_LOG_PATH_MAX - 1] = '\0';
	arg.base_path[LBD_LOG_PATH_MAX - 1] = '\0';

	if (arg.log_dir[0] == '\0') {
		pr_err("lbd: log_dir is required\n");
		return -EINVAL;
	}

	dev = kzalloc(sizeof(*dev), GFP_KERNEL);
	if (!dev)
		return -ENOMEM;

	strscpy(dev->backing_path, arg.path, sizeof(dev->backing_path));
	strscpy(dev->log_dir, arg.log_dir, sizeof(dev->log_dir));
	dev->state = LBD_STATE_UNBOUND;
	spin_lock_init(&dev->cmd_lock);
#ifdef CONFIG_LBD_MISS_HANDLER
	spin_lock_init(&dev->miss_handler_lock);
#endif
	INIT_LIST_HEAD(&dev->cmd_list);
	INIT_WORK(&dev->work, lbd_work_fn);
	mutex_init(&dev->log_mutex);
	dev->log_seq = 0;
	dev->log_has_entries = false;
	dev->log_max_size = arg.log_max_size ? arg.log_max_size
					     : LBD_LOG_MAX_SIZE_DEFAULT;
	dev->log_max_age_secs = arg.log_max_age_secs ? arg.log_max_age_secs
						      : LBD_LOG_MAX_AGE_DEFAULT;
	INIT_DELAYED_WORK(&dev->log_rotate_dwork, lbd_log_rotate_work_fn);

	dev->log_buf = kvmalloc(LBD_LOG_BUF_SIZE, GFP_KERNEL);
	if (!dev->log_buf) {
		ret = -ENOMEM;
		goto err_free;
	}
	dev->log_buf_used = 0;

	dev->lz4_state = kvmalloc(LZ4_sizeofState(), GFP_KERNEL);
	if (!dev->lz4_state) {
		ret = -ENOMEM;
		goto err_free;
	}

	/* Allocate device index */
	mutex_lock(&lbd_devices_mutex);
	idx = idr_alloc(&lbd_devices, dev, 0, 256, GFP_KERNEL);
	mutex_unlock(&lbd_devices_mutex);
	if (idx < 0) {
		ret = idx;
		goto err_free;
	}
	dev->index = idx;

	/* Setup blk-mq tag set */
	memset(&dev->tag_set, 0, sizeof(dev->tag_set));
	dev->tag_set.ops = &lbd_mq_ops;
	dev->tag_set.nr_hw_queues = 1;
	dev->tag_set.queue_depth = LBD_QUEUE_DEPTH;
	dev->tag_set.numa_node = NUMA_NO_NODE;
	dev->tag_set.cmd_size = sizeof(struct lbd_cmd);
#if LBD_HAS_QUEUE_LIMITS_API
	dev->tag_set.flags = 0;
#else
	dev->tag_set.flags = BLK_MQ_F_SHOULD_MERGE;
#endif

	ret = blk_mq_alloc_tag_set(&dev->tag_set);
	if (ret)
		goto err_idr;

	/* Allocate gendisk - version dependent */
#if LBD_HAS_QUEUE_LIMITS_API
	{
		struct queue_limits lim = {
			.logical_block_size = LBD_BLOCK_SIZE,
			.physical_block_size = LBD_BLOCK_SIZE,
			.max_hw_sectors = 256,
			.features = BLK_FEAT_WRITE_CACHE,
			.max_hw_discard_sectors = UINT_MAX >> SECTOR_SHIFT,
			.discard_granularity = LBD_BLOCK_SIZE,
		};
		dev->gd = blk_mq_alloc_disk(&dev->tag_set, &lim, dev);
	}
	if (IS_ERR(dev->gd)) {
		ret = PTR_ERR(dev->gd);
		dev->gd = NULL;
		goto err_tagset;
	}
#elif LBD_HAS_BLK_MQ_ALLOC_DISK
	dev->gd = blk_mq_alloc_disk(&dev->tag_set, dev);
	if (IS_ERR(dev->gd)) {
		ret = PTR_ERR(dev->gd);
		dev->gd = NULL;
		goto err_tagset;
	}
#else
	{
		struct request_queue *q;
		q = blk_mq_init_queue(&dev->tag_set);
		if (IS_ERR(q)) {
			ret = PTR_ERR(q);
			goto err_tagset;
		}
		dev->gd = alloc_disk(1);
		if (!dev->gd) {
			blk_cleanup_queue(q);
			ret = -ENOMEM;
			goto err_tagset;
		}
		dev->gd->queue = q;
	}
#endif

	dev->gd->major = lbd_major;
	dev->gd->first_minor = dev->index;
	dev->gd->minors = 1;
	dev->gd->fops = &lbd_fops;
	dev->gd->private_data = dev;
	snprintf(dev->gd->disk_name, DISK_NAME_LEN, "lbd%d", dev->index);

#if !LBD_HAS_BLK_MQ_ALLOC_DISK
	dev->gd->queue->queuedata = dev;
#endif

	/* Create workqueue */
	dev->wq = alloc_workqueue("lbd%d", WQ_UNBOUND | WQ_MEM_RECLAIM, 0,
				  dev->index);
	if (!dev->wq) {
		ret = -ENOMEM;
		goto err_disk;
	}

	/* Open backing file */
	dev->backing_file = filp_open(arg.path, O_RDWR | O_LARGEFILE, 0);
	if (IS_ERR(dev->backing_file)) {
		ret = PTR_ERR(dev->backing_file);
		dev->backing_file = NULL;
		pr_err("lbd: cannot open backing file '%s': %d\n", arg.path, ret);
		goto err_wq;
	}

	inode = file_inode(dev->backing_file);
	if (!S_ISREG(inode->i_mode)) {
		ret = -EINVAL;
		pr_err("lbd: backing path must be a regular file\n");
		goto err_backing;
	}

	dev->size = i_size_read(inode);
	if (dev->size == 0) {
		ret = -EINVAL;
		pr_err("lbd: backing file is empty\n");
		goto err_backing;
	}

	/* Detect qcow2-lz4 format */
	{
		u64 magic;
		loff_t magic_pos = 0;
		ssize_t mret;

		mret = kernel_read(dev->backing_file, &magic, 8, &magic_pos);
		if (mret == 8 && be64_to_cpu(magic) == LBD_QCOW2_MAGIC) {
			ret = lbd_qcow2_init(dev);
			if (ret)
				goto err_backing;
			dev->is_qcow2 = true;
			dev->size = dev->qcow2.virtual_size;
		} else {
			dev->is_qcow2 = false;
		}
	}

	/* Initialize base layer if requested */
	if (arg.base_path[0] != '\0') {
		if (!dev->is_qcow2) {
			pr_err("lbd: base layer requires qcow2-lz4 primary\n");
			ret = -EINVAL;
			goto err_backing;
		}
		strscpy(dev->base_path, arg.base_path,
			sizeof(dev->base_path));
		ret = lbd_qcow2_base_init(dev, arg.base_path);
		if (ret)
			goto err_backing;
	}

	/* Resolve log directory */
	ret = kern_path(dev->log_dir, LOOKUP_DIRECTORY, &dev->log_dir_path);
	if (ret) {
		pr_err("lbd: cannot resolve log directory '%s': %d\n",
		       dev->log_dir, ret);
		goto err_base;
	}

	/* Begin initial log segment (file created lazily on first flush) */
	lbd_begin_segment(dev);

	/* Set capacity and activate */
	set_capacity(dev->gd, dev->size >> SECTOR_SHIFT);

	/* Set queue limits (on 6.9+ these are set via struct queue_limits above) */
#if !LBD_HAS_QUEUE_LIMITS_API
	blk_queue_logical_block_size(dev->gd->queue, LBD_BLOCK_SIZE);
	blk_queue_physical_block_size(dev->gd->queue, LBD_BLOCK_SIZE);
	blk_queue_max_hw_sectors(dev->gd->queue, 256); /* 128K max per request */
	blk_queue_write_cache(dev->gd->queue, true, false);

	blk_queue_max_discard_sectors(dev->gd->queue, UINT_MAX >> SECTOR_SHIFT);
	dev->gd->queue->limits.discard_granularity = LBD_BLOCK_SIZE;
#endif

	dev->state = LBD_STATE_BOUND;

	ret = add_disk(dev->gd);
	if (ret)
		goto err_logdir;

	ret = device_add_group(disk_to_dev(dev->gd), &lbd_attr_group);
	if (ret)
		pr_warn("lbd%d: failed to create sysfs group: %d\n",
			dev->index, ret);

	/* Start age-based rotation timer */
	if (dev->log_max_age_secs > 0)
		schedule_delayed_work(&dev->log_rotate_dwork,
				      msecs_to_jiffies(dev->log_max_age_secs * 1000));

	/* Return the assigned index to userspace */
	arg.index = dev->index;
	if (copy_to_user(uarg, &arg, sizeof(arg))) {
		/* Device is live - must tear down */
		lbd_destroy_device(dev);
		mutex_lock(&lbd_devices_mutex);
		idr_remove(&lbd_devices, idx);
		mutex_unlock(&lbd_devices_mutex);
		return -EFAULT;
	}

	pr_info("lbd%d: attached to %s (%lld bytes)\n",
		dev->index, dev->backing_path, dev->size);
	return 0;

err_logdir:
	path_put(&dev->log_dir_path);
err_base:
	if (dev->base)
		lbd_qcow2_base_destroy(dev->base);
err_backing:
	if (dev->is_qcow2)
		lbd_qcow2_destroy(dev);
	fput(dev->backing_file);
err_wq:
	destroy_workqueue(dev->wq);
err_disk:
#if !LBD_HAS_BLK_MQ_ALLOC_DISK
	blk_cleanup_queue(dev->gd->queue);
#endif
	put_disk(dev->gd);
err_tagset:
	blk_mq_free_tag_set(&dev->tag_set);
err_idr:
	mutex_lock(&lbd_devices_mutex);
	idr_remove(&lbd_devices, idx);
	mutex_unlock(&lbd_devices_mutex);
err_free:
	kvfree(dev->lz4_state);
	kvfree(dev->log_buf);
	kfree(dev);
	return ret;
}

static int lbd_remove_device(struct lbd_ctl_remove __user *uarg)
{
	struct lbd_ctl_remove arg;
	struct lbd_device *dev;

	if (copy_from_user(&arg, uarg, sizeof(arg)))
		return -EFAULT;

	mutex_lock(&lbd_devices_mutex);
	dev = idr_find(&lbd_devices, arg.index);
	if (!dev) {
		mutex_unlock(&lbd_devices_mutex);
		return -ENODEV;
	}
	if (dev->state != LBD_STATE_BOUND) {
		mutex_unlock(&lbd_devices_mutex);
		return -EBUSY;
	}
	if (atomic_read(&dev->open_count) > 0) {
		mutex_unlock(&lbd_devices_mutex);
		return -EBUSY;
	}
	idr_remove(&lbd_devices, arg.index);
	mutex_unlock(&lbd_devices_mutex);

	pr_info("lbd%d: detaching\n", dev->index);
	lbd_destroy_device(dev);
	return 0;
}

static int lbd_info_device(struct lbd_ctl_info __user *uarg)
{
	struct lbd_ctl_info info;
	struct lbd_device *dev;

	if (copy_from_user(&info, uarg, sizeof(info)))
		return -EFAULT;

	mutex_lock(&lbd_devices_mutex);
	dev = idr_find(&lbd_devices, info.index);
	if (!dev) {
		mutex_unlock(&lbd_devices_mutex);
		return -ENODEV;
	}

	info.state = dev->state;
	info.size = dev->size;
	strscpy(info.path, dev->backing_path, sizeof(info.path));
	mutex_unlock(&lbd_devices_mutex);

	if (copy_to_user(uarg, &info, sizeof(info)))
		return -EFAULT;
	return 0;
}

/* ----------------------------------------------------------------
 * Sysfs stats
 * ---------------------------------------------------------------- */

static ssize_t backing_path_show(struct device *d,
				 struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%s\n", dev->backing_path);
}
static DEVICE_ATTR_RO(backing_path);

static ssize_t base_path_show(struct device *d,
			      struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%s\n", dev->base ? dev->base_path : "(none)");
}
static DEVICE_ATTR_RO(base_path);

static ssize_t log_dir_show(struct device *d,
			    struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%s\n", dev->log_dir);
}
static DEVICE_ATTR_RO(log_dir);

static ssize_t state_show(struct device *d,
			  struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;
	const char *s;

	switch (dev->state) {
	case LBD_STATE_UNBOUND:  s = "unbound";  break;
	case LBD_STATE_BOUND:    s = "bound";    break;
	case LBD_STATE_REMOVING: s = "removing"; break;
	default:                 s = "unknown";  break;
	}
	return sysfs_emit(buf, "%s\n", s);
}
static DEVICE_ATTR_RO(state);

static ssize_t device_size_show(struct device *d,
				struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", dev->size);
}
static DEVICE_ATTR_RO(device_size);

static ssize_t reads_show(struct device *d,
			  struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_reads));
}
static DEVICE_ATTR_RO(reads);

static ssize_t writes_show(struct device *d,
			   struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_writes));
}
static DEVICE_ATTR_RO(writes);

static ssize_t trims_show(struct device *d,
			  struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_trims));
}
static DEVICE_ATTR_RO(trims);

static ssize_t read_bytes_show(struct device *d,
			       struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_read_bytes));
}
static DEVICE_ATTR_RO(read_bytes);

static ssize_t write_bytes_show(struct device *d,
				struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_write_bytes));
}
static DEVICE_ATTR_RO(write_bytes);

static ssize_t trim_bytes_show(struct device *d,
			       struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_trim_bytes));
}
static DEVICE_ATTR_RO(trim_bytes);

static ssize_t alloc_reused_show(struct device *d,
				 struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_alloc_reused));
}
static DEVICE_ATTR_RO(alloc_reused);

static ssize_t alloc_new_show(struct device *d,
			      struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_alloc_new));
}
static DEVICE_ATTR_RO(alloc_new);

static ssize_t alloc_freed_show(struct device *d,
				struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_alloc_freed));
}
static DEVICE_ATTR_RO(alloc_freed);

static ssize_t compressed_show(struct device *d,
			       struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_compressed));
}
static DEVICE_ATTR_RO(compressed);

static ssize_t uncompressed_show(struct device *d,
				 struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_uncompressed));
}
static DEVICE_ATTR_RO(uncompressed);

static ssize_t log_seq_show(struct device *d,
			    struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%llu\n", READ_ONCE(dev->log_seq));
}
static DEVICE_ATTR_RO(log_seq);

static ssize_t log_segment_show(struct device *d,
				struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%s\n", dev->log_segment_label);
}
static DEVICE_ATTR_RO(log_segment);

static ssize_t log_rotations_show(struct device *d,
				  struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%lld\n", atomic64_read(&dev->stat_log_rotations));
}
static DEVICE_ATTR_RO(log_rotations);

static ssize_t log_buf_used_show(struct device *d,
				 struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;

	return sysfs_emit(buf, "%zu\n", READ_ONCE(dev->log_buf_used));
}
static DEVICE_ATTR_RO(log_buf_used);

static ssize_t segment_age_secs_show(struct device *d,
				     struct device_attribute *attr, char *buf)
{
	struct lbd_device *dev = dev_to_disk(d)->private_data;
	s64 age = ktime_to_ms(ktime_sub(ktime_get(), dev->segment_start_time));

	return sysfs_emit(buf, "%lld\n", age / 1000);
}
static DEVICE_ATTR_RO(segment_age_secs);

static struct attribute *lbd_attrs[] = {
	&dev_attr_backing_path.attr,
	&dev_attr_base_path.attr,
	&dev_attr_log_dir.attr,
	&dev_attr_state.attr,
	&dev_attr_device_size.attr,
	&dev_attr_reads.attr,
	&dev_attr_writes.attr,
	&dev_attr_trims.attr,
	&dev_attr_read_bytes.attr,
	&dev_attr_write_bytes.attr,
	&dev_attr_trim_bytes.attr,
	&dev_attr_alloc_reused.attr,
	&dev_attr_alloc_new.attr,
	&dev_attr_alloc_freed.attr,
	&dev_attr_compressed.attr,
	&dev_attr_uncompressed.attr,
	&dev_attr_log_seq.attr,
	&dev_attr_log_segment.attr,
	&dev_attr_log_rotations.attr,
	&dev_attr_log_buf_used.attr,
	&dev_attr_segment_age_secs.attr,
	NULL,
};

static const struct attribute_group lbd_attr_group = {
	.name  = "lbd",
	.attrs = lbd_attrs,
};

#ifdef CONFIG_LBD_MISS_HANDLER
/* ----------------------------------------------------------------
 * Miss handler helpers
 * ---------------------------------------------------------------- */

static struct lbd_miss_handler *lbd_miss_handler_alloc(int dev_index)
{
	struct lbd_miss_handler *mh;

	mh = kzalloc(sizeof(*mh), GFP_KERNEL);
	if (!mh)
		return NULL;

	spin_lock_init(&mh->lock);
	init_waitqueue_head(&mh->wq);
	mh->dev_index = dev_index;
	mh->pending = NULL;
	mh->has_event = false;
	return mh;
}

/*
 * Called from I/O workqueue when a cluster miss is detected.
 * Posts the miss event to the handler and blocks until userspace responds.
 * rwsem must be RELEASED before calling this.
 */
enum lbd_miss_action lbd_qcow2_handle_miss(struct lbd_device *dev,
					    u64 cluster_index)
{
	struct lbd_miss_handler *mh;
	struct lbd_miss_pending pending;

	init_completion(&pending.done);
	pending.action = LBD_MISS_CONTINUE;

	/*
	 * Hold miss_handler_lock while setting mh->pending to prevent
	 * lbd_ctl_release() from freeing mh between our read of the
	 * pointer and the registration of the pending completion.
	 * Once pending is registered, release will complete() it
	 * before kfree(mh), so mh stays alive until we wake up.
	 */
	spin_lock(&dev->miss_handler_lock);
	mh = dev->miss_handler;
	if (!mh) {
		spin_unlock(&dev->miss_handler_lock);
		return LBD_MISS_CONTINUE;
	}

	spin_lock(&mh->lock);
	mh->pending = &pending;
	mh->miss_cluster = cluster_index;
	mh->has_event = true;
	spin_unlock(&mh->lock);
	spin_unlock(&dev->miss_handler_lock);

	wake_up_interruptible(&mh->wq);

	/* Block I/O thread until userspace responds */
	wait_for_completion(&pending.done);

	return pending.action;
}
#endif /* CONFIG_LBD_MISS_HANDLER */

/* ----------------------------------------------------------------
 * Watcher helpers
 * ---------------------------------------------------------------- */

static struct lbd_watcher *lbd_watcher_alloc(int filter_dev)
{
	struct lbd_watcher *w;

	w = kzalloc(sizeof(*w), GFP_KERNEL);
	if (!w)
		return NULL;

	spin_lock_init(&w->lock);
	init_waitqueue_head(&w->wq);
	w->filter_dev = filter_dev;
	w->head = 0;
	w->tail = 0;
	w->count = 0;
	return w;
}

static void lbd_watcher_enqueue(struct lbd_watcher *w,
				const struct lbd_watcher_event *ev)
{
	spin_lock(&w->lock);
	if (w->count == LBD_WATCHER_QUEUE_SIZE) {
		/* Overflow: drop oldest */
		w->head = (w->head + 1) % LBD_WATCHER_QUEUE_SIZE;
		w->count--;
	}
	w->queue[w->tail] = *ev;
	w->tail = (w->tail + 1) % LBD_WATCHER_QUEUE_SIZE;
	w->count++;
	spin_unlock(&w->lock);
	wake_up_interruptible(&w->wq);
}

/* Copy front event without removing (for encode-then-dequeue pattern) */
static bool lbd_watcher_peek(struct lbd_watcher *w,
			     struct lbd_watcher_event *ev)
{
	bool got;

	spin_lock(&w->lock);
	got = w->count > 0;
	if (got)
		*ev = w->queue[w->head];
	spin_unlock(&w->lock);
	return got;
}

/* Remove front event after successful copy_to_user */
static void lbd_watcher_pop(struct lbd_watcher *w)
{
	spin_lock(&w->lock);
	if (w->count > 0) {
		w->head = (w->head + 1) % LBD_WATCHER_QUEUE_SIZE;
		w->count--;
	}
	spin_unlock(&w->lock);
}

/*
 * Notify all watchers of a log rotation event.
 * Called with dev->log_mutex held (sleepable).
 * Acquires lbd_watchers_lock (spin), then w->lock (spin).
 */
static void lbd_notify_watchers(struct lbd_device *dev)
{
	struct lbd_watcher_event ev;
	struct lbd_watcher *w;
	ev.dev_index = dev->index;
	ev.log_seq = dev->log_seq;
	ev.device_size = dev->size;
	memcpy(ev.segment_label, dev->log_segment_label,
	       sizeof(ev.segment_label));
	strscpy(ev.log_dir, dev->log_dir, sizeof(ev.log_dir));

	spin_lock(&lbd_watchers_lock);
	list_for_each_entry(w, &lbd_watchers, list) {
		if (w->filter_dev >= 0 && w->filter_dev != dev->index)
			continue;
		lbd_watcher_enqueue(w, &ev);
	}
	spin_unlock(&lbd_watchers_lock);
}

/* ----------------------------------------------------------------
 * Control device: watcher fops
 * ---------------------------------------------------------------- */

static int lbd_ctl_open(struct inode *inode, struct file *file)
{
	struct lbd_ctl_state *state;

	state = kzalloc(sizeof(*state), GFP_KERNEL);
	if (!state)
		return -ENOMEM;

	file->private_data = state;
	return 0;
}

static int lbd_ctl_release(struct inode *inode, struct file *file)
{
	struct lbd_ctl_state *state = file->private_data;

	if (!state)
		return 0;

	if (state->watcher) {
		spin_lock(&lbd_watchers_lock);
		list_del(&state->watcher->list);
		spin_unlock(&lbd_watchers_lock);
		kfree(state->watcher);
	}

#ifdef CONFIG_LBD_MISS_HANDLER
	if (state->miss) {
		struct lbd_miss_handler *mh = state->miss;
		struct lbd_device *dev;

		/* Find the device and clear its handler pointer */
		mutex_lock(&lbd_devices_mutex);
		dev = idr_find(&lbd_devices, mh->dev_index);
		if (dev) {
			spin_lock(&dev->miss_handler_lock);
			if (dev->miss_handler == mh)
				dev->miss_handler = NULL;
			spin_unlock(&dev->miss_handler_lock);
		}
		mutex_unlock(&lbd_devices_mutex);

		/* Complete any pending miss with CONTINUE */
		spin_lock(&mh->lock);
		if (mh->pending) {
			mh->pending->action = LBD_MISS_CONTINUE;
			complete(&mh->pending->done);
			mh->pending = NULL;
		}
		spin_unlock(&mh->lock);

		kfree(mh);
	}
#endif

	kfree(state);
	file->private_data = NULL;
	return 0;
}

static ssize_t lbd_ctl_write(struct file *file, const char __user *ubuf,
			     size_t count, loff_t *ppos)
{
	struct lbd_ctl_state *state = file->private_data;
	u8 kbuf[512];
	struct cbor_dec d;
	u64 map_count, key;
	char cmd[16];
	int filter_dev = -1;
	bool have_cmd = false;
	char path[LBD_LOG_PATH_MAX];
	bool have_path = false;
	u64 i;

	if (!state)
		return -EINVAL;

	if (count > sizeof(kbuf))
		return -EINVAL;

	if (copy_from_user(kbuf, ubuf, count))
		return -EFAULT;

	cbor_dec_init(&d, kbuf, count);

	if (cbor_dec_map(&d, &map_count))
		return -EINVAL;

	path[0] = '\0';

	for (i = 0; i < map_count; i++) {
		if (cbor_dec_uint(&d, &key))
			return -EINVAL;

		switch (key) {
		case LBD_WATCH_KEY_CMD:
			if (cbor_dec_text(&d, cmd, sizeof(cmd), NULL))
				return -EINVAL;
			have_cmd = true;
			break;
		case LBD_WATCH_KEY_DEV: {
			u64 dev_val;
			if (cbor_dec_uint(&d, &dev_val))
				return -EINVAL;
			filter_dev = (int)dev_val;
			break;
		}
		case LBD_WATCH_KEY_PATH:
			if (cbor_dec_text(&d, path, sizeof(path), NULL))
				return -EINVAL;
			have_path = true;
			break;
		default:
			return -EINVAL;
		}
	}

	if (!have_cmd)
		return -EINVAL;

	if (strcmp(cmd, "watch") == 0) {
		struct lbd_watcher *w;

		if (state->watcher)
			return -EBUSY;

		w = lbd_watcher_alloc(filter_dev);
		if (!w)
			return -ENOMEM;

		spin_lock(&lbd_watchers_lock);
		list_add_tail(&w->list, &lbd_watchers);
		spin_unlock(&lbd_watchers_lock);

		state->watcher = w;
		return count;
	}

#ifdef CONFIG_LBD_MISS_HANDLER
	if (strcmp(cmd, "manage_misses") == 0) {
		struct lbd_device *dev;
		struct lbd_miss_handler *mh;

		if (state->miss)
			return -EBUSY;
		if (filter_dev < 0)
			return -EINVAL;

		mh = lbd_miss_handler_alloc(filter_dev);
		if (!mh)
			return -ENOMEM;

		mutex_lock(&lbd_devices_mutex);
		dev = idr_find(&lbd_devices, filter_dev);
		if (!dev) {
			mutex_unlock(&lbd_devices_mutex);
			kfree(mh);
			return -ENODEV;
		}
		if (!dev->is_qcow2) {
			mutex_unlock(&lbd_devices_mutex);
			kfree(mh);
			return -EINVAL;
		}

		spin_lock(&dev->miss_handler_lock);
		if (dev->miss_handler) {
			spin_unlock(&dev->miss_handler_lock);
			mutex_unlock(&lbd_devices_mutex);
			kfree(mh);
			return -EBUSY;
		}
		dev->miss_handler = mh;
		spin_unlock(&dev->miss_handler_lock);
		mutex_unlock(&lbd_devices_mutex);

		state->miss = mh;
		return count;
	}

	if (strcmp(cmd, "continue") == 0) {
		struct lbd_miss_handler *mh = state->miss;

		if (!mh)
			return -EINVAL;

		spin_lock(&mh->lock);
		if (!mh->pending) {
			spin_unlock(&mh->lock);
			return -EINVAL;
		}
		mh->pending->action = LBD_MISS_CONTINUE;
		complete(&mh->pending->done);
		mh->pending = NULL;
		mh->has_event = false;
		spin_unlock(&mh->lock);

		return count;
	}

	if (strcmp(cmd, "retry") == 0) {
		struct lbd_miss_handler *mh = state->miss;

		if (!mh)
			return -EINVAL;

		spin_lock(&mh->lock);
		if (!mh->pending) {
			spin_unlock(&mh->lock);
			return -EINVAL;
		}
		mh->pending->action = LBD_MISS_RETRY;
		complete(&mh->pending->done);
		mh->pending = NULL;
		mh->has_event = false;
		spin_unlock(&mh->lock);

		return count;
	}

	if (strcmp(cmd, "swap") == 0) {
		struct lbd_device *dev;
		struct lbd_miss_handler *mh = state->miss;
		int ret;

		if (!mh)
			return -EINVAL;
		if (!have_path || path[0] == '\0')
			return -EINVAL;

		mutex_lock(&lbd_devices_mutex);
		dev = idr_find(&lbd_devices, mh->dev_index);
		if (!dev) {
			mutex_unlock(&lbd_devices_mutex);
			return -ENODEV;
		}
		mutex_unlock(&lbd_devices_mutex);

		ret = lbd_qcow2_swap_base(dev, path);
		if (ret)
			return ret;

		return count;
	}
#endif /* CONFIG_LBD_MISS_HANDLER */

	return -EINVAL;
}

static bool lbd_ctl_has_event(struct lbd_ctl_state *state)
{
	struct lbd_watcher *w = state->watcher;
	struct lbd_watcher_event ev;

#ifdef CONFIG_LBD_MISS_HANDLER
	struct lbd_miss_handler *mh = state->miss;

	if (mh) {
		bool has;

		spin_lock(&mh->lock);
		has = mh->has_event;
		spin_unlock(&mh->lock);
		if (has)
			return true;
	}
#endif
	if (w && lbd_watcher_peek(w, &ev))
		return true;
	return false;
}

static ssize_t lbd_ctl_read(struct file *file, char __user *ubuf,
			    size_t count, loff_t *ppos)
{
	struct lbd_ctl_state *state = file->private_data;
	struct lbd_watcher *w;
	u8 tmp[512];
	struct cbor_enc e;
	size_t encoded_len;
#ifdef CONFIG_LBD_MISS_HANDLER
	struct lbd_miss_handler *mh;
#endif

	if (!state)
		return -EINVAL;

	w = state->watcher;

#ifdef CONFIG_LBD_MISS_HANDLER
	mh = state->miss;

	if (!mh && !w)
		return -EINVAL;
#else
	if (!w)
		return -EINVAL;
#endif

	/* Block until an event is available */
	if (!lbd_ctl_has_event(state)) {
		int ret;

		if (file->f_flags & O_NONBLOCK)
			return -EAGAIN;

#ifdef CONFIG_LBD_MISS_HANDLER
		if (mh) {
			ret = wait_event_interruptible(mh->wq,
				lbd_ctl_has_event(state));
		} else {
			ret = wait_event_interruptible(w->wq,
				lbd_ctl_has_event(state));
		}
#else
		ret = wait_event_interruptible(w->wq,
			lbd_ctl_has_event(state));
#endif
		if (ret)
			return -ERESTARTSYS;
	}

#ifdef CONFIG_LBD_MISS_HANDLER
	/* Miss events take priority (time-critical, I/O thread blocked) */
	if (mh) {
		bool has;

		spin_lock(&mh->lock);
		has = mh->has_event;
		spin_unlock(&mh->lock);

		if (has) {
			u64 cluster;

			spin_lock(&mh->lock);
			cluster = mh->miss_cluster;
			spin_unlock(&mh->lock);

			cbor_enc_init(&e, tmp, sizeof(tmp));
			cbor_enc_map(&e, 3);

			cbor_enc_uint(&e, LBD_MISS_KEY_TYPE);
			cbor_enc_text(&e, "block_miss", 10);

			cbor_enc_uint(&e, LBD_MISS_KEY_DEV);
			cbor_enc_uint(&e, (u64)mh->dev_index);

			cbor_enc_uint(&e, LBD_MISS_KEY_CLUSTER);
			cbor_enc_uint(&e, cluster);

			if (e.err)
				return -EINVAL;

			encoded_len = cbor_enc_len(&e);
			if (count < encoded_len)
				return -EINVAL;

			if (copy_to_user(ubuf, tmp, encoded_len))
				return -EFAULT;

			return encoded_len;
		}
	}
#endif /* CONFIG_LBD_MISS_HANDLER */

	/* Fall through to watcher events */
	if (w) {
		struct lbd_watcher_event ev;
		size_t label_len, dir_len;

		if (!lbd_watcher_peek(w, &ev)) {
			if (file->f_flags & O_NONBLOCK)
				return -EAGAIN;

			if (wait_event_interruptible(w->wq,
						     lbd_watcher_peek(w, &ev)))
				return -ERESTARTSYS;
		}

		cbor_enc_init(&e, tmp, sizeof(tmp));
		cbor_enc_map(&e, 6);

		cbor_enc_uint(&e, LBD_EVENT_KEY_TYPE);
		cbor_enc_text(&e, "log_rotated", 11);

		cbor_enc_uint(&e, LBD_EVENT_KEY_DEV);
		cbor_enc_uint(&e, (u64)ev.dev_index);

		cbor_enc_uint(&e, LBD_EVENT_KEY_LABEL);
		label_len = strlen(ev.segment_label);
		cbor_enc_text(&e, ev.segment_label, label_len);

		cbor_enc_uint(&e, LBD_EVENT_KEY_DIR);
		dir_len = strlen(ev.log_dir);
		cbor_enc_text(&e, ev.log_dir, dir_len);

		cbor_enc_uint(&e, LBD_EVENT_KEY_SEQ);
		cbor_enc_uint(&e, ev.log_seq);

		cbor_enc_uint(&e, LBD_EVENT_KEY_SIZE);
		cbor_enc_uint(&e, ev.device_size);

		if (e.err)
			return -EINVAL;

		encoded_len = cbor_enc_len(&e);
		if (count < encoded_len)
			return -EINVAL;

		if (copy_to_user(ubuf, tmp, encoded_len))
			return -EFAULT;

		lbd_watcher_pop(w);
		return encoded_len;
	}

	return -EAGAIN;
}

static __poll_t lbd_ctl_poll(struct file *file,
			     struct poll_table_struct *wait)
{
	struct lbd_ctl_state *state = file->private_data;
	struct lbd_watcher *w;
	__poll_t mask = 0;
#ifdef CONFIG_LBD_MISS_HANDLER
	struct lbd_miss_handler *mh;
#endif

	if (!state)
		return 0;

	w = state->watcher;

#ifdef CONFIG_LBD_MISS_HANDLER
	mh = state->miss;

	if (!mh && !w)
		return 0;

	if (mh)
		poll_wait(file, &mh->wq, wait);
#else
	if (!w)
		return 0;
#endif
	if (w)
		poll_wait(file, &w->wq, wait);

#ifdef CONFIG_LBD_MISS_HANDLER
	if (mh) {
		spin_lock(&mh->lock);
		if (mh->has_event)
			mask |= EPOLLIN | EPOLLRDNORM;
		spin_unlock(&mh->lock);
	}
#endif

	if (w) {
		spin_lock(&w->lock);
		if (w->count > 0)
			mask |= EPOLLIN | EPOLLRDNORM;
		spin_unlock(&w->lock);
	}

	return mask;
}

/* ----------------------------------------------------------------
 * Control device (misc device)
 * ---------------------------------------------------------------- */

static long lbd_ctl_ioctl(struct file *file, unsigned int cmd,
			   unsigned long arg)
{
	switch (cmd) {
	case LBD_CTL_ADD:
		return lbd_add_device((struct lbd_ctl_add __user *)arg);
	case LBD_CTL_REMOVE:
		return lbd_remove_device((struct lbd_ctl_remove __user *)arg);
	case LBD_CTL_INFO:
		return lbd_info_device((struct lbd_ctl_info __user *)arg);
	default:
		return -ENOTTY;
	}
}

static const struct file_operations lbd_ctl_fops = {
	.owner		= THIS_MODULE,
	.open		= lbd_ctl_open,
	.release	= lbd_ctl_release,
	.read		= lbd_ctl_read,
	.write		= lbd_ctl_write,
	.poll		= lbd_ctl_poll,
	.unlocked_ioctl	= lbd_ctl_ioctl,
	.compat_ioctl	= compat_ptr_ioctl,
};

static struct miscdevice lbd_misc = {
	.minor	= MISC_DYNAMIC_MINOR,
	.name	= LBD_CTL_NAME,
	.fops	= &lbd_ctl_fops,
};

/* ----------------------------------------------------------------
 * Module init / exit
 * ---------------------------------------------------------------- */

static int __init lbd_init(void)
{
	int ret;

	lbd_major = register_blkdev(0, LBD_NAME);
	if (lbd_major < 0) {
		pr_err("lbd: failed to register block device\n");
		return lbd_major;
	}

	idr_init(&lbd_devices);

	ret = misc_register(&lbd_misc);
	if (ret) {
		pr_err("lbd: failed to register control device\n");
		idr_destroy(&lbd_devices);
		unregister_blkdev(lbd_major, LBD_NAME);
		return ret;
	}

	pr_info("lbd: module loaded (major=%d)\n", lbd_major);
	return 0;
}

static void __exit lbd_exit(void)
{
	struct lbd_device *dev;
	int id;

	misc_deregister(&lbd_misc);

	mutex_lock(&lbd_devices_mutex);
	idr_for_each_entry(&lbd_devices, dev, id) {
		idr_remove(&lbd_devices, id);
		mutex_unlock(&lbd_devices_mutex);
		lbd_destroy_device(dev);
		mutex_lock(&lbd_devices_mutex);
	}
	mutex_unlock(&lbd_devices_mutex);

	idr_destroy(&lbd_devices);
	unregister_blkdev(lbd_major, LBD_NAME);

	pr_info("lbd: module unloaded\n");
}

module_init(lbd_init);
module_exit(lbd_exit);
