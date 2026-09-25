ifneq ($(KERNELRELEASE),)
# Called from kernel build system
obj-m := lbd.o
lbd-y := lbd_main.o lbd_qcow2.o lz4/lz4.o

# Newer kernels re-add explicit old_parent/new_parent dentry fields to
# struct renamedata, which vfs_rename() dereferences directly. Distro version
# numbers don't reliably mark when this landed, so probe the headers for the
# field and let lbd_rename_file() populate the parents when it is present.
LBD_RENAME_PARENT := $(shell grep -qs 'old_parent' $(srctree)/include/linux/fs.h && echo 1 || echo 0)

ccflags-y := -include $(M)/lz4_kcompat.h -DLBD_HAS_RENAME_PARENT=$(LBD_RENAME_PARENT)
CFLAGS_lz4/lz4.o := -Wno-deprecated-declarations -Wframe-larger-than=32768

else
# Called from command line
KDIR ?= /lib/modules/$(shell uname -r)/build

all: lbd.ko lbdctl

lbd.ko: lbd_main.c lbd.h lbd_qcow2.c lbd_qcow2.h lbd_qcow2_format.h cbor_enc.h cbor_dec.h lz4_kcompat.h lz4/lz4.c lz4/lz4.h
	$(MAKE) -C $(KDIR) M=$(CURDIR) KBUILD_MODPOST_WARN=1 modules

lbdctl: lbdctl.c lbd.h lbd_qcow2_format.h lz4/lz4.c lz4/lz4.h
	$(CC) -Wall -Wextra -O2 -o $@ lbdctl.c lz4/lz4.c

clean:
	$(MAKE) -C $(KDIR) M=$(CURDIR) clean
	rm -f lbdctl

.PHONY: all clean

endif
