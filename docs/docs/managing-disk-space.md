---
title: Managing Disk Space
description: How Miren reclaims disk space automatically — version and image retention, build cache, logs and metrics, and what happens under disk pressure.
keywords: [disk space, garbage collection, retention, image cleanup, disk pressure, storage]
---

# Managing Disk Space

Every deploy leaves something behind on the node: a container image, a snapshot, some registry blobs, build cache, logs. On a cluster that deploys often, all of that accumulates, and a node with a finite disk will eventually fill up. Miren reclaims this space automatically and continuously, so under normal use you never have to think about it. This page is the map for when you do — what accumulates, how each piece gets cleaned up, what you can tune, and what changes when the disk gets tight.

## What uses disk

A handful of things grow over time:

- **App versions and their images.** Every deploy mints a new version, and each version pins a container image, its overlayfs snapshot, and the registry blobs behind it. On a frequently-deployed cluster this is usually the largest and fastest-growing consumer.
- **Build cache.** BuildKit keeps a layer cache to make rebuilds fast.
- **Logs and metrics.** Application logs (VictoriaLogs) and metrics (VictoriaMetrics) are stored on the node with their own retention windows.
- **Persistent disks.** Your app's own data on local storage or Miren Disks. This is yours to size; Miren never reclaims it out from under you.

## How Miren reclaims space automatically

Each consumer has its own janitor running on its own schedule. Nothing here needs a cron job or a manual sweep — it all runs in the background.

| What accumulates | Reclaimed by | Default policy | Tune with |
|------------------|-------------|----------------|-----------|
| App versions | Version retention GC (hourly) | Keep the active version, the most-recent `retention_count` (10), and anything younger than `retention_period` (30d) | [`[app_version]`](/server-config#app-version) |
| Images, snapshots, registry blobs | Image + blob GC (weekly, plus hourly under pressure) | Reclaim images whose version has been pruned | follows version retention |
| Build cache | BuildKit | Cap at `gc_keep_storage` (10GB), evict entries older than `gc_keep_duration` (7d) | [`[buildkit]`](/server-config#buildkit) |
| Logs | VictoriaLogs | Keep `retention_period` (30d) | [`[victorialogs]`](/server-config#victorialogs) |
| Metrics | VictoriaMetrics | Keep `retention_period` (1 month) | [`[victoriametrics]`](/server-config#victoriametrics) |
| Preview versions | Ephemeral GC (every 5 min) | Delete past their TTL (default 24h), cap 10 per app | `--ttl` at deploy |
| Deleted persistent disks | Deleted-volume GC (hourly) | Purge 7 days after deletion | see [Persistent Storage](/disks) |

The rest of this section walks through the two that matter most on a busy cluster; the others are covered by their config reference.

### App versions and their images

Version retention runs hourly and keeps, per app, three things: the active version, the most-recent `retention_count` versions, and any version younger than `retention_period`. Anything else is a candidate for deletion — except a version a running or pending sandbox still references, which is always kept until that sandbox is gone.

The part worth understanding is that deleting a version does *not* immediately free its image. Reclamation is a chain, and each link is a separate background controller running on its own schedule:

1. **Version GC deletes the version.** That removes the only thing linking the version to its underlying image artifact.
2. **Artifact GC notices the orphan.** On its next sweep it sees an image artifact that no surviving version references, and marks it archived. Because images are deduplicated by content, this happens only after the *last* version using that image is gone.
3. **Image GC deletes the archived image**, releasing its overlayfs snapshot so containerd can free the layers.
4. **Blob GC removes the registry blobs** that backed it.

So reclaiming an image is eventually-consistent: after a version ages out, its disk comes back over the next few cleanup cycles (the image and blob step runs weekly unless disk pressure pulls it forward), not the instant it's pruned. This is by design — every hop re-checks that nothing has started using the version or image in the meantime, so nothing in use is ever pulled out from under a running app.

### Preview versions

Preview (ephemeral) deploys are handled separately. Each gets a TTL at deploy time (default 24h, set with `--ttl`), and a sweep every five minutes deletes the ones that have expired, tearing down their sandbox pools and letting their images flow through the same cascade above. Miren also caps previews at 10 per app, retiring the oldest as new ones arrive. See [Pull Request Environments](/pr-environments) for the full preview workflow.

## Under disk pressure

Here's the catch that the automatic policy alone doesn't cover. Under normal operation, `retention_period` keeps roughly `deploys-per-day × 30 days` of versions around, and every one of them pins an image. Those versions haven't aged out, so the cascade above never starts for them, and the image GC has nothing to reclaim even as the disk fills. The age window is doing its job — it's just that on a high-deploy cluster the job is keeping *too much*.

So both the image GC and the version GC watch disk usage, and at **80%** they escalate:

- The **image GC**, which otherwise runs weekly, checks usage hourly and runs a full sweep the moment it crosses the threshold.
- The **version GC** drops its `retention_period` floor for that sweep. Retention collapses to the active version plus `retention_count` (and anything a live sandbox still pins), the surplus recent versions become deletable, and the cascade finally reclaims their images.

This is where the two retention knobs stop being equivalent. `retention_count` (plus the active version) is a **hard floor**, honored on every sweep regardless of pressure. `retention_period` is **best-effort**: it keeps younger versions around when there's room, but a disk-pressure sweep sheds it. Size `retention_count` for the history you genuinely can't lose, and treat `retention_period` as a convenience that yields when the disk is tight.

The 80% threshold is the same for both collectors, kept in sync so version pruning and image reclamation escalate together rather than one starving while the other waits.

## When your disk is filling up

The automatic policy handles the steady state, but if a node is running hot, here's where to look and what to reach for:

- **Images from frequent deploys** are the usual culprit. You don't have to wait for the 80% trigger — lower `retention_period` (or lean on `retention_count`) in [`[app_version]`](/server-config#app-version) to age versions out sooner, and the cascade reclaims their images on the next few sweeps.
- **Build cache** growing past what you expect: tighten `gc_keep_storage` in [`[buildkit]`](/server-config#buildkit).
- **Logs or metrics** eating space: shorten the `retention_period` for [`[victorialogs]`](/server-config#victorialogs) or [`[victoriametrics]`](/server-config#victoriametrics).
- **Persistent disk data** is your application's own, and Miren won't reclaim it. If a deleted disk is still holding space, remember there's a 7-day undelete window before the deleted-volume GC purges it for good — see [Persistent Storage](/disks).
