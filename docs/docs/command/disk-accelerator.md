---
title: "miren disk accelerator"
sidebar_label: "disk accelerator"
description: "Faster block-device disks via the lbd kernel module"
---

# miren disk accelerator

Faster block-device disks via the lbd kernel module

Miren serves block-device disks in one of two modes.

**Universal mode** is the default and works everywhere. It backs each disk with a
loop device, which the Linux kernel provides out of the box.

**Accelerator mode** uses `lbd`, a Miren kernel module that puts a
write-ahead log in front of the disk. It is faster, and it is what continuous
backup to Miren Cloud is built on.

`lbd` is not part of the Linux kernel, so it has to be compiled for the
exact kernel your node is running. `miren disk accelerator install` does
that for you: it downloads a builder image, compiles the module against your
kernel's headers inside a container, then installs and loads the result. Nothing
but Miren has to be installed on the host beyond the kernel headers themselves.

## Getting started

```bash
miren disk accelerator status          # can this host run it?
sudo miren disk accelerator install    # build and load the module
sudo systemctl restart miren           # pick up the new mode
```

## Requirements

- The kernel headers for your running kernel. On Debian and Ubuntu the builder
  fetches them itself if the host has none. Everywhere else you install them
  first, and `status` names the package -- `kernel-devel-$(uname -r)`
  on Fedora and RHEL.
- Secure Boot disabled. A self-built module is unsigned, and firmware with Secure
  Boot enforcing will refuse to load it.
- A kernel built with GCC. Clang-built kernels are not supported.

## After a kernel upgrade

A module only loads on the kernel it was built for. Once a host has installed the
module, Miren notices on startup that the running kernel has changed and rebuilds
it. You can also do it by hand with
`sudo miren disk accelerator install --force`.

Until the module is back, disks fall back to universal mode. Nothing breaks; they
are just slower.

## Usage

```bash
miren disk accelerator [flags]
```

## Subcommands

- [`miren disk accelerator install`](/command/disk-accelerator-install) — Build and load the lbd kernel module for this kernel
- [`miren disk accelerator status`](/command/disk-accelerator-status) — Show whether accelerator mode can run on this host
- [`miren disk accelerator uninstall`](/command/disk-accelerator-uninstall) — Unload and remove the lbd kernel module

## See also

- [`miren disk`](/command/disk)
