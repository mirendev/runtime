---
title: VictoriaLogs upgrade and rollback
description: How Miren backs up embedded VictoriaLogs data before upgrading and restores it on rollback.
---

# VictoriaLogs upgrade and rollback

Miren embeds VictoriaLogs for log storage. The upgrade from v1.0 to v1.52
crosses incompatible storage format changes. The component now takes a
hardlink backup of the **stopped** data directory before starting a changed
image or spec, including when a graceful shutdown already removed the old
container. If backup creation fails, startup stops rather than risk migrating
unprotected data. The live directory remains `<server.data_path>/victorialogs`;
backups live alongside it as `victorialogs.backup-<spec fingerprint>` (or
`victorialogs.backup-legacy` for a pre-fingerprint installation). Keep enough
free inodes and disk space for new writes and merges: hardlinks initially
share file contents, but parts later removed by VictoriaLogs retention or merges
remain on disk as long as a backup references them. Over time, each snapshot
can cost as much disk as the store contained when it was taken. Monitor free
space during the rollback window.

:::warning[Data directory placement]

Keep `victorialogs` as a real directory on the same filesystem as its parent
`server.data_path`. A symlink at `server.data_path/victorialogs` cannot be
snapshotted safely, and a separate filesystem mounted at that path prevents
hardlinking and renaming the data directory. Either layout prevents Miren from
booting when a backup is needed. Mount or symlink at `server.data_path` instead,
or bind-mount the parent data path rather than `victorialogs` itself. The live
directory and its backup siblings must share a filesystem, and `victorialogs`
must remain renameable. Move the data while Miren is stopped, preserving its
ownership and permissions, before restarting.

:::

Miren records the order of images used with the data. On a change back to an
earlier image, it stops VictoriaLogs, moves the upgraded directory aside as
`victorialogs.replaced-<fingerprint>`, restores the matching backup, and starts
the requested image. Miren prefers a matching spec snapshot, but can use the
most recent snapshot of that image if port or retention changed since the
upgrade. A snapshot without an image record cannot be matched this way; if no
snapshot matches, Miren warns that the requested image may not open the current
data. A later move forward to a newer image migrates the live store instead of
restoring an old snapshot of that newer image, so logs collected during the
rollback remain available. Repeated rollbacks keep additional upgraded stores
under `victorialogs.replaced-<fingerprint>-<timestamp>`. The backup remains
available for another restore.
Logs ingested after the backup was taken will not appear in the restored store;
they remain in the moved-aside directory. Miren does not delete backups or
replaced directories automatically. If a spec is revisited, a subsequent image
upgrade refreshes its backup from current stopped data and retains the older
copy with a `.superseded-<timestamp>` suffix. There is no automatic newest-three
limit: port or retention changes also create backups, so a count-only limit
could delete the only snapshot of an earlier image needed for rollback. Monitor
disk use and remove stale backups **only after** verifying which image snapshots
are still needed. In particular, `backup-legacy` is needed to roll back to a
pre-backup Miren release; remove it only after accepting that rollback is no
longer possible. Remove old `superseded-*` and `replaced-*` directories once
their historical logs and forward-retry data are no longer needed. Deletion is
manual because Miren cannot infer when an operator has closed the rollback
window.

:::danger[Rolling back to a pre-backup Miren release]

An older Miren binary does not understand these backups. In particular, a
rollback to a release embedding VictoriaLogs v1.0 is **not** a binary-only
rollback: it cannot safely open the v1.52 directory. This also applies when
`miren upgrade` automatically rolls back to that release after VictoriaLogs
has migrated its data; the old server may fail to start with
`FATAL: unsupported part format version` in the VictoriaLogs logs. The
automatic rollback does not restore the VictoriaLogs backup. Stop Miren,
verify that VictoriaLogs and its container/rootfs snapshot have stopped and
been removed, move the upgraded data directory aside, then restore the stopped
`victorialogs.backup-legacy` directory to `victorialogs` while preserving its
ownership and permissions. Only then start the old Miren release. If the
backup is absent or cannot be restored, do not start the old binary on the
upgraded directory.

:::

This applies only when `[victorialogs].start_embedded = true`. Miren does not
manage external VictoriaLogs storage. Attribute-wide `miren logs -g` searches
require VictoriaLogs v1.50 or newer; Miren warns at startup if an external
server reports an older version or its version cannot be verified. Upgrade an
external server independently before relying on all-field results.
