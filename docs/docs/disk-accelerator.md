---
title: Disk Accelerator
description: Turn on accelerator mode for Miren Disks by building and loading the lbd kernel module against your node's running kernel.
keywords: [accelerator, lbd, kernel module, disk performance, disk mode, loop device]
---

# Disk Accelerator

Miren serves [Miren Disks](/disks#miren-disks) in one of two modes, and picks
between them on its own.

**Universal mode** is the default and works everywhere. It backs each disk with a
*loop device*, which is the kernel's built-in way of presenting a file as though
it were a disk. Nothing to install, no configuration.

**Accelerator mode** uses `lbd`, a Miren kernel module that puts a write-ahead
log in front of the disk. Writes land in the log first and are folded into the
disk image behind them, which makes writes faster and gives Miren an exact,
ordered record of every change. That record is what continuous backup to Miren
Cloud is built on.

`lbd` is not part of Linux, so it has to be compiled for the exact kernel your
node is running. One command does that.

## Minimum working example

```bash
miren disk accelerator status          # can this host run it?
sudo miren disk accelerator install    # build and load the module
sudo systemctl restart miren           # pick up the new mode
```

`install` downloads a builder image, compiles the module against your kernel's
headers inside a container, then installs and loads the result. The toolchain
lives in the image, so on Debian and Ubuntu there is nothing to install first —
if the host has no kernel headers, the builder fetches them for itself.

Once the server restarts, new disks use accelerator mode. Existing disks keep
whatever mode they were created with.

## Requirements

| Requirement | Why | If it is missing |
|---|---|---|
| Kernel headers for the running kernel | The module is compiled against them | On Debian and Ubuntu the builder fetches them; elsewhere `status` names the package |
| Secure Boot off | A self-built module is unsigned, and enforcing firmware refuses it | `install` stops and says so |
| A GCC-built kernel | The builder ships GCC, not Clang | `install` stops and says so |
| Root | Loading a kernel module needs it | Run under `sudo` |

### About the headers

The builder image is Debian-based, so on a Debian or Ubuntu host it can install
`linux-headers-$(uname -r)` for itself and you need nothing on the host. That
needs a route to the distribution's package archive from the node, and it can
still come up empty for a kernel too new or too old to be in the archive — the
build says so, naming the package it could not find.

On any other distribution, install the headers yourself first.
`miren disk accelerator status` prints the exact package name; it is
`kernel-devel-$(uname -r)` on Fedora and RHEL.

Installing the headers on the host is always the faster path, because the
builder then borrows them read-only instead of downloading them, and needs no
network at all.

## Checking what is going on

```bash
miren disk accelerator status
```

```
Available            yes
State                lbd v0.0.0-20260824210626-be4cec661034 is loaded for kernel 6.8.0-51-generic
Kernel               6.8.0-51-generic
Module loaded        yes
Control device       yes
Module installed     yes
lbdctl               /usr/local/bin/lbdctl
Kernel headers       /lib/modules/6.8.0-51-generic/build
Bundled lbd version  v0.0.0-20260824210626-be4cec661034
```

`--format json` gives the same thing as machine-readable fields.

**Available** is the answer to the only question that matters: can this node
serve accelerator disks right now. It is true only when the module is loaded,
its control device exists, and `lbdctl` is installed to drive it. Any one of
those missing puts disks back on loop devices.

## After a kernel upgrade

A module only loads on the kernel it was built for, so a kernel upgrade leaves
the installed module unusable.

Miren handles this. On startup it notices the running kernel no longer matches
the module it built, and rebuilds. You do not have to do anything, though you
can force it by hand:

```bash
sudo miren disk accelerator install --force
```

This only happens on hosts that installed the module in the first place. A host
that never turned accelerator mode on never pays for an unattended compile at
startup.

Until the module is back, disks fall back to universal mode. Nothing breaks;
they are just slower.

## Choosing the mode yourself

Auto-detection can be overridden in the server config:

```toml title="/etc/miren/server.toml"
disk_mode = "universal"    # or "accelerator", or "auto" (the default)
```

`universal` forces loop devices even where the module is loaded. `accelerator`
insists on `lbd`, and disks will fail to attach if it is not there — useful when
you would rather find out loudly than quietly run slower. See
[Server Configuration](/server-config).

## Turning it off

```bash
sudo miren disk accelerator uninstall
sudo systemctl restart miren
```

This unloads the module, removes it along with `lbdctl`, and forgets that the
host ever had it, so nothing rebuilds it later. It fails if a disk is still
attached — the kernel will not unload a module in use. Stop the apps holding
disks first.
