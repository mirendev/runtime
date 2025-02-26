
if [ "$(uname -p)" = "aarch64" ]; then
  kernel=https://github.com/lab47/isle-kernel/releases/download/v20240217/linux-6.6.15-arm64.tar.xz
  root=https://dl-cdn.alpinelinux.org/alpine/v3.15/releases/aarch64/alpine-minirootfs-3.15.2-aarch64.tar.gz
  initrd=https://github.com/lab47/isle-kernel/releases/download/v20220327/initrd-aarch64
else
  kernel=https://github.com/lab47/isle-kernel/releases/download/v20220327/linux-5.16.17-x86.tar.xz
  root=https://dl-cdn.alpinelinux.org/alpine/v3.15/releases/x86_64/alpine-minirootfs-3.15.2-x86_64.tar.gz
  initrd=https://github.com/lab47/isle-kernel/releases/download/v20220327/initrd-x86_64
fi

ROOT="${ROOT:-rootfs}"
TMP="${TMP:-/tmp}"

rm -rf "$ROOT"
mkdir -p "$ROOT"

mkdir -p release

echo "+ Downloading assets"

if ! test -e release/initrd; then
  curl -o release/initrd -L $initrd
fi

if ! test -e $TMP/rootfs.tar.gz; then
  curl -o $TMP/rootfs.tar.gz -L $root
fi

if ! test -e $TMP/kernel.tar.xz; then
  curl -o $TMP/kernel.tar.xz -L $kernel
fi

echo "+ Unpacking rootfs"
pushd "$ROOT"
tar xf $TMP/rootfs.tar.gz
popd

echo "+ Applying custom code"
cp -a custom/* "$ROOT"

chown root.root -R "$ROOT"

echo "+ Add package"

cp /etc/resolv.conf "$ROOT/etc/resolv.conf"

chroot "$ROOT" apk add --no-cache

rm "$ROOT/etc/resolv.conf"

echo "+ Adding kernel models"

tar -C "$ROOT" -xf $TMP/kernel.tar.xz

gunzip < "$ROOT"/boot/vmlinuz-* > release/vmlinux

# We don't use these and they just take up space.
rm -rf "$ROOT"/boot/vmlinu*

KERNEL_VERSION=$(ls $ROOT/lib/modules | head -n 1)

echo "- Running depmod to be sure modules are ready"

chroot "$ROOT" depmod -ae -F /boot/System.map-$KERNEL_VERSION $KERNEL_VERSION

rm release/os.fs
mksquashfs "$ROOT" release/os.fs -comp xz
