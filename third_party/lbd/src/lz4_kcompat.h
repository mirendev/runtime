/* SPDX-License-Identifier: GPL-2.0 */
#ifndef _LZ4_KCOMPAT_H
#define _LZ4_KCOMPAT_H

#ifdef __KERNEL__
#define LZ4_FREESTANDING 1
#define LZ4_memcpy  __builtin_memcpy
#define LZ4_memmove __builtin_memmove
#define LZ4_memset  __builtin_memset
#define LZ4_STATIC_LINKING_ONLY_DISABLE_MEMORY_ALLOCATION 1
#include <linux/types.h>
#endif

#include "lz4/lz4.h"

#endif /* _LZ4_KCOMPAT_H */
