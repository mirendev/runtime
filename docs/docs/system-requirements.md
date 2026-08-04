---
title: System Requirements
description: Minimum and recommended hardware for running a Miren server or runner — OS, architecture, memory, and storage.
keywords: [system requirements, hardware, linux, memory, disk space, arm64, x86, runner]
---

import CliCommand from '@site/src/components/CliCommand';

# System Requirements

Miren needs a Linux server with enough memory and disk space to run its components and build your applications.

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| **Operating System** | Linux (kernel 5.10+) | |
| **Architecture** | x86_64 or arm64 | |
| **Memory** | 4 GB | 8 GB |
| **Storage** | 50 GB | 100 GB |

## Why these numbers?

### Memory

Miren runs several components — containerd, etcd, buildkit, metrics, and logging — that together use around 600 MB of memory at idle. During builds, memory usage spikes as buildkit compiles your application. A single Rails app with Postgres can push total usage past 1.3 GB during deployment, which is why we set the minimum at 4 GB.

With 8 GB, you'll have comfortable headroom for running multiple apps and handling concurrent builds without things getting tight.

### Storage

Container images and build caches add up quickly. Base images for languages like Ruby or Python are 50-80 MB compressed but expand on disk, and BuildKit caches intermediate build layers aggressively — keeping up to 10 GB by default. A single Rails deployment can use 15-20 GB between base images, build cache, and the container registry. With multiple apps and their version history, usage grows from there. Miren reclaims images, caches, and old versions automatically as space gets tight — see [Managing Disk Space](/managing-disk-space).

Starting with 50 GB gives you enough room to get going. With 100 GB you'll have space to grow without worrying about "no space left on device" errors during builds.

## Runner nodes

The same numbers apply to every machine you add as a [distributed runner](/distributed-runners). `miren runner install` runs the same check against the same minimums.

The reasoning shifts a little, though. A runner doesn't build your apps or hold cluster state, so it never runs buildkit or etcd, and it won't see the build-time memory spikes that set the server's floor. What it does run is containerd and every sandbox the scheduler places on it, and it pulls and stores the images those sandboxes need. So on a runner, plan memory around how many sandboxes you expect to land there, and storage around the images they pull rather than the build cache.

## What happens if my system is too small?

The `miren server install` and `miren runner install` commands check your system against these requirements before installing. If your machine doesn't meet the minimums, the installer will let you know what's short and point you here.

If you're below the recommended thresholds but above the minimums, you'll see a heads-up but installation will proceed normally.

If you know what you're doing and want to install anyway (say, for testing), you can bypass the check:

<CliCommand context="server">

```miren
sudo miren server install --skip-system-check
```

</CliCommand>

`miren runner install` takes the same flag, which is worth knowing if you're enrolling runners from a provisioning script that can't answer a prompt.

## We'd love to hear from you

We're still learning about system requirements as more people deploy Miren in different contexts. If you have an interesting deployment scenario or resource constraints you'd like to discuss, come chat with us on [Discord](https://miren.dev/discord)!
