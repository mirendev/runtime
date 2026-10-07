#!/usr/bin/env bash
# Sync the lbd kernel module source from the miren.dev/lbd module into
# third_party/lbd, so it can be embedded in the miren binary and handed to the
# builder image.
#
# The version is whatever go.mod pins -- never a hardcoded string here. Run
# without arguments to update the checked-in tree; run with --check to verify it
# matches, which is what CI does.

set -euo pipefail

cd "$(dirname "$0")/.."

# Only src/ is generated. third_party/lbd also holds a hand-written README.md
# and embed.go, which this script must leave alone.
DEST="third_party/lbd/src"
MODULE="miren.dev/lbd"

check_only=0
if [ "${1:-}" = "--check" ]; then
	check_only=1
elif [ -n "${1:-}" ]; then
	echo "usage: $0 [--check]" >&2
	exit 2
fi

version="$(go list -m -f '{{.Version}}' "$MODULE")"
if [ -z "$version" ]; then
	echo "could not resolve the $MODULE version from go.mod" >&2
	exit 1
fi

# Ensure the module is in the cache, then ask go where it landed. The cache is
# read-only, so everything copied out of it needs its mode fixed up.
go mod download "$MODULE"
src="$(go list -m -f '{{.Dir}}' "$MODULE")/src"
if [ ! -d "$src" ]; then
	echo "no src/ directory in $MODULE $version (looked in $src)" >&2
	exit 1
fi

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

# Everything the module build needs, and nothing else: the module's own C, the
# vendored LZ4, the Makefile, and dkms.conf. README.md and test_lbd.sh are
# developer files that belong in the lbd repo, not in the binary.
for f in \
	Makefile \
	dkms.conf \
	lbd.h \
	lbd_main.c \
	lbd_qcow2.c \
	lbd_qcow2.h \
	lbd_qcow2_format.h \
	lbdctl.c \
	cbor_dec.h \
	cbor_enc.h \
	lz4_kcompat.h \
	lz4/lz4.c \
	lz4/lz4.h; do
	if [ ! -f "$src/$f" ]; then
		echo "$MODULE $version is missing src/$f" >&2
		echo "the file list in $0 needs updating to match the module" >&2
		exit 1
	fi
	mkdir -p "$staging/$(dirname "$f")"
	install -m 0644 "$src/$f" "$staging/$f"
done

printf '%s\n' "$version" >"$staging/VERSION"

if [ "$check_only" -eq 1 ]; then
	if diff -ru "$DEST" "$staging"; then
		echo "$DEST is in sync with $MODULE $version"
		exit 0
	fi
	echo >&2
	echo "$DEST does not match $MODULE $version -- run hack/sync-lbd-src.sh" >&2
	exit 1
fi

rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
cp -R "$staging" "$DEST"
echo "synced $DEST from $MODULE $version"
