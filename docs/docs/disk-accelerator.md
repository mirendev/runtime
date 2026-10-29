---
title: Disk Accelerator
description: Turn on accelerator mode for Miren Disks by building and loading the lbd kernel module against your node's running kernel.
keywords: [accelerator, lbd, kernel module, disk performance, disk mode, loop device]
---

# Disk Accelerator

Miren serves [Miren Disks](./disks.md#miren-disks) in one of two modes, and picks
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

:::warning[Experimental]
Accelerator mode is experimental. It's here for you to try, and we'd love to hear how it goes, but expect rough edges and changes between releases. Universal mode remains the default.
:::

## Minimum working example

```bash
miren disk accelerator install runner1     # build and load the module there
sudo systemctl restart miren               # on that node, to pick up the mode
```

`install` names the node to install on, because the module has to be compiled
against the kernel that node is running. Your cluster builds the toolchain image
itself, using the same BuildKit and registry it already uses for your apps, and
the node pulls it from there. Nothing is downloaded from us, and there is no
image to keep up to date.

The toolchain lives in that image, so on Debian and Ubuntu there is nothing to
install on the host first — if it has no kernel headers, the builder fetches
them for itself.

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

`status` reads the host you run it on, not the node you last installed to. To
see how a runner is doing, run it on that runner.

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
miren disk accelerator install runner1 --force
```

This only happens on hosts that installed the module in the first place. A host
that never turned accelerator mode on never pays for an unattended compile at
startup.

When a Miren upgrade bundles a newer `lbd` but the installed module still
works, startup leaves it running. Replacing a working module automatically
could strand disks if the new one fails to load. Drain disks from the node,
then run `miren disk accelerator install <node>` to upgrade it deliberately.

The startup rebuild is capped at ten minutes. If it runs over, the server logs a
warning, carries on booting, and the node stays on loop devices until you run
`install` yourself. Accelerator mode is a speed-up, so a build that will not
finish must never hold up a node coming back after a reboot.

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
[Server Configuration](./server-config.md).

## Turning it off

```bash
sudo miren disk accelerator uninstall
sudo systemctl restart miren
```

This unloads the module, removes it along with `lbdctl`, and forgets that the
host ever had it, so nothing rebuilds it later. It fails if a disk is still
attached — the kernel will not unload a module in use. Stop the apps holding
disks first.

## Recovering a blocked cloud upload

:::warning[Upgrade cloud before runtime]

Every cloud replica serving this runtime must support checksum-bound upload
recovery, the dedicated `/prepare` endpoint, and idempotent `/lease/acquire`.
Upgrade cloud first, including staging and alternate cloud endpoints. Do not
roll cloud back to an incompatible version while this runtime is in use:
missing upload support blocks uploads and lease transitions, and acquisition
has no legacy fallback.

:::

An unresolved upload batch keeps a durable pending-upload gate on its cloud
volume. Lease acquisition and release remain blocked across runtime restarts:
changing the lease mid-upload could invalidate the batch's authorization or
allow a different writer before its outcome is known.

For transient network or cloud failures, retain the original log segments and
let the uploader retry. Checksum-bound begin requests recover the original
upload identities, including a lost begin response. Ordered, authenticated
completion of **every** original segment confirms the batch before the runtime
clears the gate. It retains the volume lock through durable horizon updates and
local file retirement.

Normal unmount closes the final LBD segment and drains the retained logs under
the original lease before releasing it. A failed detach or drain keeps the
lease and local recovery state; restart retries the same originals. This avoids
depending on nonce-less tail uploads after another runner takes ownership.

Orphan lease cleanup also requires the owning runner to confirm its containers
are gone. An unreachable runner or a legacy lease with no recorded owner is
not proof of absence and is not automatically released. Preserve that fencing
until ownership can be established, or escalate for protocol-level recovery.

If payload mismatch or lease-nonce rejection persists:

- Stop app writes through normal sandbox shutdown; preserve the disk image,
  original log segments, replay horizon, and `diskio-state.json`. Treat the
  persisted credentials as secrets.
- Investigate the cloud volume, original payloads, and current lease ownership.
  Recovery must use the original checksum-bound identities and exact completion
  metadata, in order, with valid authorization. Completion retries can publish
  pending updates; they are not read-only status probes.

:::danger[Do not bypass upload fencing]

Do not clear `pending_cloud_uploads`, replace the nonce, advance the horizon,
or delete retained segments to unblock the controller. An active listing,
missing update, generic 403/409/404, or successful generic update deletion does
not prove that the whole batch has safely completed or aborted.

:::

There is currently no supported forced-abort/unblock command. The cloud API
does not provide an authenticated, conditional batch abort that fences late
requests and proves all pending identities resolved. If normal completion
cannot reconcile the original batch, the volume intentionally remains blocked;
escalate for protocol-level recovery rather than bypassing local fencing.
