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
exact kernel each node is running. Pass `--disk-accelerator` to
`miren server install` to enable it before the server's first start.
Once the cluster is running, `miren disk accelerator install` infers
the node from the local server or runner configuration, or you can name a
running runner to install remotely. The coordinator builds the toolchain
image in its registry, and the target node builds and loads the module.
The lbd toolchain image is not published, so it does not need a release of its own.

## Getting started

```bash
sudo miren server install --disk-accelerator  # install and start with lbd ready
# Or, once the cluster is running, on this server or runner:
sudo miren disk accelerator install
sudo systemctl restart miren              # use miren-runner on a joined runner
# Or, from a client, name a running runner:
miren disk accelerator install runner1   # build and load it on runner1
sudo systemctl restart miren-runner       # on runner1, to pick up the mode
```

With no node argument, the command reads the joined runner ID if configured,
or the installed native server's configured runner ID. Run it on that host;
if there is no local identity, pass a node name or ID. The server or runner
must be running for the install RPC to reach it. The joined runner config is
normally root-readable only. When selecting a cluster with
`-C`, pass the node explicitly rather than inferring one from this host.
`status` and `uninstall` always operate on this host.

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
`miren disk accelerator install --force` on that running host.

Until the module is back, disks fall back to universal mode. Nothing breaks; they
are just slower.

## Usage

```bash
miren disk accelerator [flags]
```

## Subcommands

- [`miren disk accelerator install`](./disk-accelerator-install.md) — Build and load the lbd kernel module on a running node
- [`miren disk accelerator status`](./disk-accelerator-status.md) — Show whether accelerator mode can run on this host
- [`miren disk accelerator uninstall`](./disk-accelerator-uninstall.md) — Unload and remove the lbd kernel module

## See also

- [`miren disk`](./disk.md)
