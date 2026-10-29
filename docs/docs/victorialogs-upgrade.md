---
title: VictoriaLogs upgrade and rollback
description: How Miren backs up embedded VictoriaLogs data before upgrading and restores it on rollback.
---

# VictoriaLogs upgrade and rollback

Miren embeds VictoriaLogs for log storage. The upgrade from v1.0 to v1.52
crosses incompatible storage format changes, so v1.52 keeps its data in a
directory of its own, `<server.data_path>/victorialogs-v2`, and Miren never
writes to the v1.0 directory, `<server.data_path>/victorialogs`, again.

## Upgrading from Miren v0.16 or earlier

On first boot, Miren stops any VictoriaLogs still running on
`victorialogs` and creates `victorialogs-v2` from it with hardlinks, so the
copy costs no extra space at first. VictoriaLogs v1.52 then migrates only
the copy. VictoriaLogs never modifies a file it has written: it adds new
parts, removes merged ones, and replaces its part lists by rename. Linking
therefore leaves every file in `victorialogs` exactly as v1.0 wrote it.

That is what makes rollback safe, including the automatic rollback that
`miren upgrade` performs when the new server fails to become ready. The
older release opens `victorialogs` as it was at the upgrade and starts
normally. Logs collected while the newer release ran stay in
`victorialogs-v2` and are not visible to the older release.

If you upgrade again after a rollback, Miren notices that the older release
has written to `victorialogs` since the seed. It sets the previous
`victorialogs-v2` aside as `victorialogs-v2.replaced-<timestamp>` and seeds a
fresh copy, so the logs from the rollback carry forward. If the older release
did not write anything, the existing `victorialogs-v2` is used as is.

`victorialogs` keeps the disk space of the v1.0 store, because nothing applies
retention to it anymore. Once you no longer need to roll back to Miren v0.16
or earlier, stop Miren and remove it, along with any
`victorialogs-v2.replaced-*` directories you no longer need.

:::note[Clusters that ran a pre-release build]

A cluster that ran a main-channel build between the VictoriaLogs v1.52
upgrade and this change migrated `victorialogs` in place. Its `victorialogs`
already holds v1.52 data, which is carried forward into `victorialogs-v2` the
same way, and its `victorialogs.backup-legacy` remains the only v1.0 copy.
To roll such a cluster back to v0.16, follow the manual restore at the end of
this page.

:::

## Later image changes

Within `victorialogs-v2`, the component takes a hardlink backup of the
**stopped** data directory before starting a changed image or spec, including
when a graceful shutdown already removed the old container. If backup creation
fails, startup stops rather than risk migrating unprotected data. Backups live
alongside it as `victorialogs-v2.backup-<spec fingerprint>`. Keep enough
free inodes and disk space for new writes and merges: hardlinks initially
share file contents, but parts later removed by VictoriaLogs retention or merges
remain on disk as long as a backup references them. Over time, each snapshot
can cost as much disk as the store contained when it was taken. Monitor free
space during the rollback window.

:::warning[Data directory placement]

Keep `victorialogs` and `victorialogs-v2` as real directories on the same
filesystem as their parent `server.data_path`. A symlink at either path cannot
be linked safely, and a separate filesystem mounted at either path prevents
hardlinking and renaming the data directory. Either layout prevents Miren from
booting when a seed or backup is needed. Mount or symlink at
`server.data_path` instead, or bind-mount the parent data path rather than a
VictoriaLogs directory itself. Every VictoriaLogs directory and its backup
siblings must share a filesystem, and each must remain renameable. Move the
data while Miren is stopped, preserving its ownership and permissions, before
restarting.

:::

Miren records the order of images used with the data. On a change back to an
earlier image, it stops VictoriaLogs, moves the upgraded directory aside as
`victorialogs-v2.replaced-<fingerprint>`, restores the matching backup, and starts
the requested image. Miren prefers a matching spec snapshot, but can use the
most recent snapshot of that image if port or retention changed since the
upgrade. A snapshot without an image record cannot be matched this way; if no
snapshot matches, Miren warns that the requested image may not open the current
data. A later move forward to a newer image migrates the live store instead of
restoring an old snapshot of that newer image, so logs collected during the
rollback remain available. Repeated rollbacks keep additional upgraded stores
under `victorialogs-v2.replaced-<fingerprint>-<timestamp>`. The backup remains
available for another restore.
Logs ingested after the backup was taken will not appear in the restored store;
they remain in the moved-aside directory. Miren does not delete backups or
replaced directories automatically. If a spec is revisited, a subsequent image
upgrade refreshes its backup from current stopped data and retains the older
copy with a `.superseded-<timestamp>` suffix. There is no automatic newest-three
limit: port or retention changes also create backups, so a count-only limit
could delete the only snapshot of an earlier image needed for rollback. Monitor
disk use and remove stale backups **only after** verifying which image snapshots
are still needed. Remove old `superseded-*` and `replaced-*` directories once
their historical logs and forward-retry data are no longer needed. Deletion is
manual because Miren cannot infer when an operator has closed the rollback
window.

:::danger[Manual rollback of a migrated `victorialogs`]

This applies only to a cluster whose `victorialogs` was migrated in place
(see the note above). A release embedding VictoriaLogs v1.0 cannot open that
directory and fails to start with `FATAL: unsupported part format version` in
the VictoriaLogs logs. Stop Miren, verify that VictoriaLogs and its
container/rootfs snapshot have stopped and been removed, move `victorialogs`
aside, then restore the stopped `victorialogs.backup-legacy` directory to
`victorialogs` while preserving its ownership and permissions. Only then start
the old Miren release. If the backup is absent or cannot be restored, do not
start the old binary on the upgraded directory.

:::
