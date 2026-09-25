#!/usr/bin/env bash
# Compile the lbd kernel module and lbdctl against the running kernel.
#
# Runs inside the lbd-builder image; see docker/Dockerfile.lbd-builder for the
# mounts and environment it expects. Everything it prints is shown to the
# operator, so messages here are the ones they will act on.

set -euo pipefail

SRC=${SRC:-/src}
OUT=${OUT:-/out}

fail() {
	echo "error: $*" >&2
	exit 1
}

[ -d "$SRC" ] || fail "no module source at $SRC"
[ -f "$SRC/Makefile" ] || fail "$SRC has no Makefile; the source mount looks wrong"
mkdir -p "$OUT"

# A container shares the host's kernel, so uname and /proc/version already
# describe the machine we are building for.
KERNEL_RELEASE=${KERNEL_RELEASE:-$(uname -r)}
[ -n "$KERNEL_RELEASE" ] || fail "could not determine the kernel release"

# ---------------------------------------------------------------------------
# Kernel headers
# ---------------------------------------------------------------------------

find_headers() {
	local candidate
	for candidate in \
		"/lib/modules/$KERNEL_RELEASE/build" \
		"/usr/src/kernels/$KERNEL_RELEASE" \
		"/usr/src/linux-headers-$KERNEL_RELEASE"; do
		if [ -f "$candidate/Makefile" ]; then
			echo "$candidate"
			return 0
		fi
	done
	return 1
}

header_package() {
	local id
	for id in "${HOST_DISTRO_ID:-}" ${HOST_DISTRO_LIKE:-}; do
		case "$id" in
		debian | ubuntu) echo "linux-headers-$KERNEL_RELEASE"; return 0 ;;
		fedora | rhel | centos) echo "kernel-devel-$KERNEL_RELEASE"; return 0 ;;
		arch | alpine) echo "linux-headers"; return 0 ;;
		suse | opensuse* | sles) echo "kernel-devel"; return 0 ;;
		esac
	done
	return 1
}

KDIR=${KERNEL_HEADERS:-}
if [ -n "$KDIR" ] && [ ! -f "$KDIR/Makefile" ]; then
	echo "warning: $KDIR is not a kernel build tree; looking elsewhere" >&2
	KDIR=""
fi
if [ -z "$KDIR" ]; then
	KDIR=$(find_headers || true)
fi
# No build tree came in from the host. miren leaves /lib/modules and /usr/src
# unmounted in that case, so the builder can install headers into its own
# filesystem instead. This only reaches a Debian-family archive, because that is
# what this image is built from.
if [ -z "$KDIR" ] && [ "${FETCH_HEADERS:-0}" = "1" ]; then
	pkg="linux-headers-$KERNEL_RELEASE"
	echo "No kernel headers on the host; fetching $pkg"

	if ! apt-get update -qq; then
		fail "could not reach the package archive to fetch $pkg. Install it on the host and try again."
	fi

	if ! DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$pkg"; then
		fail "$pkg is not available from the package archive, which usually means this kernel is too new or too old for it. Install the headers on the host and try again."
	fi

	KDIR=$(find_headers || true)
	if [ -z "$KDIR" ]; then
		fail "$pkg installed but left no build tree for $KERNEL_RELEASE"
	fi
	echo "Fetched kernel headers into $KDIR"
fi

if [ -z "$KDIR" ]; then
	pkg=$(header_package || true)
	if [ -n "$pkg" ]; then
		fail "no kernel headers for $KERNEL_RELEASE. Install $pkg on the host and try again."
	fi
	fail "no kernel headers for $KERNEL_RELEASE. Install this kernel's headers on the host and try again."
fi

echo "Building lbd for kernel $KERNEL_RELEASE against $KDIR"

# ---------------------------------------------------------------------------
# Compiler
#
# A module should be built by roughly the compiler that built the kernel. A
# major-version gap is usually only a modpost warning, but a Clang-built kernel
# with control-flow integrity rejects a GCC-built module outright -- better to
# say so than to hand back a module that silently will not load.
# ---------------------------------------------------------------------------

kernel_compiler_line=$(cat /proc/version 2>/dev/null || echo "")

if echo "$kernel_compiler_line" | grep -qi 'clang version'; then
	fail "this kernel was built with Clang, which the lbd builder does not support. Install the module from your distribution instead, or build it on the host."
fi

want_major=$(echo "$kernel_compiler_line" | grep -oE '\bgcc[^0-9]*([0-9]+)' | grep -oE '[0-9]+' | head -1 || true)

pick_gcc() {
	local want=$1 candidate best=""
	if [ -n "$want" ] && command -v "gcc-$want" >/dev/null 2>&1; then
		echo "gcc-$want"
		return 0
	fi
	# No exact match: take the newest installed major and warn. Ordering here
	# is oldest to newest so the last hit wins.
	for candidate in 12 13 14 15 16; do
		command -v "gcc-$candidate" >/dev/null 2>&1 && best="gcc-$candidate"
	done
	if [ -n "$best" ]; then
		echo "$best"
		return 0
	fi
	command -v gcc >/dev/null 2>&1 && echo gcc && return 0
	return 1
}

CC=$(pick_gcc "$want_major") || fail "no C compiler in the builder image"

if [ -n "$want_major" ] && [ "$CC" != "gcc-$want_major" ]; then
	echo "warning: this kernel was built with gcc-$want_major but the builder only has $CC." >&2
	echo "warning: the module should still load, but report it if modprobe refuses it." >&2
elif [ -z "$want_major" ]; then
	echo "warning: could not tell which compiler built this kernel; using $CC" >&2
fi

echo "Compiling with $CC ($($CC -dumpversion))"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

# The module's Makefile passes KBUILD_MODPOST_WARN=1, which downgrades
# unresolved symbols to warnings. That means a zero exit code alone does not
# prove the module will load, so the log is checked below.
log=$(mktemp)
if ! make -C "$SRC" KDIR="$KDIR" CC="$CC" lbd.ko 2>&1 | tee "$log"; then
	fail "compiling lbd.ko failed"
fi

if grep -q 'undefined!' "$log"; then
	grep 'undefined!' "$log" >&2
	fail "lbd.ko references symbols this kernel does not export, so it would fail to load"
fi

# lbdctl is linked statically: it runs on the host, whose libc is not the
# builder image's.
if ! make -C "$SRC" CC="$CC -static" lbdctl; then
	fail "compiling lbdctl failed"
fi

# ---------------------------------------------------------------------------
# Verify and hand back
# ---------------------------------------------------------------------------

[ -f "$SRC/lbd.ko" ] || fail "the build reported success but produced no lbd.ko"
[ -f "$SRC/lbdctl" ] || fail "the build reported success but produced no lbdctl"

# vermagic is what the kernel checks at load time. If it disagrees with the
# running kernel, modprobe will refuse the module, so catch it here where we
# can explain why.
vermagic=$(modinfo -F vermagic "$SRC/lbd.ko" 2>/dev/null || echo "")
case "$vermagic" in
"$KERNEL_RELEASE"*) ;;
"") echo "warning: could not read vermagic from lbd.ko" >&2 ;;
*) fail "lbd.ko was built for '$vermagic' but this host runs $KERNEL_RELEASE; the headers at $KDIR do not match the running kernel" ;;
esac

install -m 0644 "$SRC/lbd.ko" "$OUT/lbd.ko"
install -m 0755 "$SRC/lbdctl" "$OUT/lbdctl"

echo "Built lbd.ko ($(stat -c %s "$OUT/lbd.ko") bytes) and lbdctl for $KERNEL_RELEASE"
