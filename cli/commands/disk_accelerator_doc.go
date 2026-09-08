package commands

const acceleratorSectionDescription = `Miren serves block-device disks in one of two modes.

**Universal mode** is the default and works everywhere. It backs each disk with a
loop device, which the Linux kernel provides out of the box.

**Accelerator mode** uses ` + "`lbd`" + `, a Miren kernel module that puts a
write-ahead log in front of the disk. It is faster, and it is what continuous
backup to Miren Cloud is built on.

` + "`lbd`" + ` is not part of the Linux kernel, so it has to be compiled for the
exact kernel each node is running. ` + "`miren disk accelerator install`" + ` does
that: your cluster builds the toolchain image with the BuildKit and registry it
already runs, the named node pulls it from there, and the module is compiled and
loaded on that node. Nothing is downloaded from us, and there is no published
image to keep up to date.

## Getting started

` + "```" + `bash
miren disk accelerator status          # can this host run it?
miren disk accelerator install runner1  # build and load it there
sudo systemctl restart miren           # pick up the new mode
` + "```" + `

## Requirements

- The kernel headers for your running kernel. On Debian and Ubuntu the builder
  fetches them itself if the host has none. Everywhere else you install them
  first, and ` + "`status`" + ` names the package -- ` + "`kernel-devel-$(uname -r)`" + `
  on Fedora and RHEL.
- Secure Boot disabled. A self-built module is unsigned, and firmware with Secure
  Boot enforcing will refuse to load it.
- A kernel built with GCC. Clang-built kernels are not supported.

## After a kernel upgrade

A module only loads on the kernel it was built for. Once a host has installed the
module, Miren notices on startup that the running kernel has changed and rebuilds
it. You can also do it by hand with
` + "`miren disk accelerator install <node> --force`" + `.

Until the module is back, disks fall back to universal mode. Nothing breaks; they
are just slower.`
