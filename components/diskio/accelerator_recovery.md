# Recovery extensions to volume updates (MIR-2184 / MIR-2185)

This proposes additive recovery fields on the existing volume update protocol,
not a separate accelerator publication/catalog API. Upload claims, completion,
cursor pagination and downloads retain their existing endpoints. MIR-2185 must
implement and jointly test the extensions before accelerator prefix retention
can be enabled. MIR-2183 owns shared retirement and restore protection, including
the proposed session operations below; those operations are **not implemented
in cloud merely because this client exists**. Ordinary listings without recovery
metadata cannot authorize recovery or GC.

**This change is purely additive.** Legacy replay (`replayMissingSegments`)
remains the default mount path, and runtime and cloud can deploy in either order.
Legacy replay is unsafe only once cloud retires log prefixes, which does not
exist yet. Protected recovery is used only when the volume already has an
`accelerator-stream` file **and** the cloud supports restore sessions; a
404/405/501 on `restore-sessions` or a `status: "unsupported"` listing falls
back to legacy replay. A cloud without the route answers its POST with 405,
because the path also matches `DELETE /updates/{updateId}`. Other errors from a v1 cloud still fail closed. MIR-2185's first cloud
change accepts checkpoints but does not yet build restore sessions or the
`recovery_version=1` listing; those return 405 or 404, so stream volumes take the
legacy replay fallback until they exist.

**Deferred to a later change:** removing the legacy fallback for stream volumes,
enabling prefix retirement, and using protected recovery on a node with no
local stream file. These follow cloud retention (MIR-2185) and a passing restore
onto a fresh node from cloud data alone, against the real cloud.

## Recovery

All paths below are relative to `/api/v1/disk/volumes/{volume}/updates` and use
the existing authenticated JSON transport:

* Proposed shared `POST /restore-sessions`: accepts `version: 1`, `lease_nonce`,
  and `kind: "lbd_checkpoint"` to select and protect an immutable checkpoint
  plus its full tail in the same transaction as selection. Returns `version`,
  `volume_id`, `session_id`, and `expires_at`. The protection primitive is shared
  with full-image restore; checkpoint-plus-tail is one selection kind, not a
  separate session implementation. Existing full-image callers are unchanged
  by this runtime patch; MIR-2183 supplies their shared protection integration.
* Existing `GET /?kind=lbd_log&restore_session=...&recovery_version=1`: returns
  the existing `updates` and `next_cursor`, plus a `recovery` object containing
  the `RecoveryManifest` fields other than `segments`. Every page repeats the
  same selection metadata and binds to the requested session. Tail rows retain
  `update_id`, `kind`, `ordering_key`, and `size`, adding `sha256`,
  `previous_label`, `first_sequence`, and `entry_count`. The runtime assembles
  the manifest from all pages using the existing cursor walk. `complete: true`,
  `segment_count`, and `head` describe the **whole** selected tail, not one page.
  Missing/changed metadata, repeated cursors, early EOF, and missing middle or
  terminal rows fail closed. The existing 1,000-page guard returns an error,
  never a successful truncated result.
* Proposed shared `POST /restore-sessions/{session}/renew`: accepts `version: 1`
  and returns the same identity fields with a new `expires_at`. Renewal extends
  protection of the same selection, including its base and entire tail,
  transactionally against GC. An expired session cannot be revived. A client
  that loses protection restarts selection rather than reusing old URLs.
* Existing `GET /{update}/download` retrieves the selected base and tail bytes.
  The session pins all selected objects throughout listing and downloading;
  resolving an individual download URL is not itself sufficient GC protection.

`status` is `ready` or `never_backed_up`; `unsupported` sends runtime back to
legacy replay. Other statuses, including `expired` and `unavailable`, are
errors, never permission to initialize an empty filesystem. Runtime only asks
for a stream it already has, so it treats `never_backed_up` as lost history.
The stream itself is proposed by runtime in its first checkpoint and becomes
durable only when cloud acknowledges that completion; a retried first checkpoint
reuses the same proposed `stream_id` from its staged intent. `ready` requires a base, stream incarnation, device size, floor, and head. Each object
has its exact payload size and lowercase SHA-256. The checkpoint also binds the
uncompressed image's size/SHA-256 and its inclusive `through_label`.

Predecessors must come from the durable ordered activation ledger. Constructing
them from the objects remaining after deletion would hide gaps. Entry counts
and first sequence numbers must be bound to the verified immutable LBD payload.
Sequence numbers advance within a segment; LBD resets them on attachment, so
cross-attachment ordering comes from the activation ledger, not sequence alone.
The cloud must fence stream changes and ordered log activation using the writer
lease. Retired objects cannot be resurrected by an old upload retry.

The runtime checks the selected base is at or above the coverage floor and
that the tail links from its inclusive boundary all the way to the head. It
checks transport length/digest, snapshot size/checksum, LBD version, label,
device size, entry counts/sequences, bounds, operations, and write CRCs. It
renews protection during downloads and again before committing installation.
Sessions may expire naturally after completion; v1 requires no release endpoint.

## Checkpoint production and publication

Checkpoints are taken after unmount, successful LBD detach, and final-log drain,
while still holding the existing cloud-volume lock and writer lease, and only
when the cloud advertises `recovery_version: 1` on upload begin. A legacy volume
(no stream file) proposes a fresh `stream_id` in its first checkpoint; the stream
file is written only once cloud acknowledges it. That first clean detach is the
migration; there is no separate step. A detach whose `log_horizon` equals the
last acknowledged boundary uploads nothing. These are filesystem
crash-consistent images; application-level consistency requires the application
to flush/quiesce before unmount. A manual `loop_image` backup, timestamp, or
`was_attached: false` is not a checkpoint proof.

The producer stages a `miren-snapshot` payload with a durable intent, then uses
the existing checksum-bound `updates` upload protocol with kind
`lbd_checkpoint`. Its 16-lowercase-hex ordering key is a stable content/boundary
identity, **not a chronological timestamp**. This kind must be excluded from
full-image selection and ordinary full-image retirement.

Existing `POST /upload` includes `recovery_version: 1` and `stream_id` for
checkpoints: the volume's recorded stream, or the stream a legacy volume
proposes in its first checkpoint. Its response must explicitly acknowledge the
same version before runtime sends bytes or completion. Runtime reads a 400, 404
or 501 on checkpoint begin as an old cloud and stops checkpointing for the rest
of the process, so a v1 cloud must reject an invalid checkpoint claim with
another status, such as 422. Lease refusals are 403.

**Cloud refuses a conflicting `stream_id` with 409 at upload begin, before any
upload URL exists**, so no bytes are sent. At both begin and completion, runtime
treats a 409 as a stream conflict only when the body carries
`{"code": "stream_conflict"}`. A plain 409 also covers duplicate ordering keys or
snapshot names, constraint violations, and differing payloads or metadata, so it
is an ordinary best-effort failure: no persistent skip, the lease is released,
and at begin the staged bytes are discarded so the next detach restages them.
Existing `POST /{update}/complete` carries the normal checksum,
size and lease fields plus a typed `checkpoint` (`CheckpointPublication`), binding
volume, stream, update ID, payload size/SHA-256, inclusive boundary and image
size/SHA-256. Completion must:

1. Authenticate the same writer lease and stream incarnation.
2. Verify the uploaded payload's kind, size, MD5/CRC32C, and the snapshot
   header's image size and SHA-256. The payload SHA-256 is the writer's claim:
   cloud does not hash the whole payload within the completion deadline, so
   runtime re-checks it on every download before using the bytes.
3. Verify the inclusive boundary equals the committed stream head, with no
   unresolved upload or log gap. A local `log_horizon` is not sufficient proof.
4. Activate the update and its recovery relationship atomically, preserving all
   existing recovery points and active restore sessions. There is no intermediate
   completed-but-unpublished checkpoint and no second publication request.

The completion response must echo the exact accepted `checkpoint`. An HTTP 2xx
without that acknowledgement is not success. A repeat completion must bind the
same coverage to the same checksum-bound update, including after a committed
response is lost. `/prepare`, when used, only prepares bytes; it cannot activate
checkpoint coverage. Checkpoint fields are indexed activation data, not merely
opaque `metadata` stored in an object sidecar.

Checkpoints are **best-effort**. A failure is logged and the lease is still
released; no pending-upload marker is set, so later attaches and log uploads are
never refused. The drained tail is already durable, so a missed checkpoint loses
nothing while cloud retains logs. Staged bytes and intent survive for the next
detach, which reuses the same claim if the boundary is unchanged and restages
otherwise. An old cloud's refusal (unknown kind, 404, or no version
acknowledgement) discards the staged checkpoint and disables checkpoints for the
rest of the process.

A stream conflict at begin or completion discards the staged checkpoint and records the refused local
stream (empty for a legacy volume) in `accelerator-checkpoint-conflict`. Later
detaches skip checkpoints while the stream file still matches it, including
after a restart, so a full image is not uploaded on every detach only to be
refused. The marker clears when the local stream changes, such as after a
successful protected recovery writes the stream file.

The detach checkpoint holds the writer lease, so it is capped by a timeout
(default 120 seconds; `accelerator_checkpoint_timeout` in server config or
runner config, or `MIREN_SERVER_ACCELERATOR_CHECKPOINT_TIMEOUT`). Compression and
upload both stop at the deadline. The staged bytes are discarded and the lease
is released. The intent is kept, because it carries a legacy volume's proposed
stream, which cloud may already have accepted; the next detach restages under
it. Hourly checkpoints while the disk is attached (MIR-2213) cover the gap left
by detach checkpoints that time out. Neither upload completion nor publication deletes any cloud
history; cloud retirement remains MIR-2185's transaction.

## Installation, compatibility, and cost

For stream volumes on a v1 cloud, recovery deliberately downloads a selected
base on every mount, then applies its full tail to a temporary image. This is more
I/O than trusting a local horizon, but gives a small correctness boundary without
introducing a local checkpoint cache protocol. A matching stream's local crash
tail is drained before selection. A stream reset, or a `never_backed_up` answer
for a volume that has a stream, fails rather than silently starting over.

Only after validation and disk sync does the runtime persist an install intent,
rename the image, and durably install the horizon and stream. An interrupted
installation is rebuilt from a new protected selection; its partially installed
files are never mounted as a completed recovery. Sparse trailing zeros retain
the declared physical image length.

Old clouds may ignore new request fields, so runtime requires explicit capability,
completion and protected-listing acknowledgements rather than trusting HTTP
success. Before enabling prefix retirement, cloud must retain legacy chains and
reject writers/restorers that still fall back to legacy replay; advertising
support on one runner is not proof that all future restorers support it.
Universal-mode backup/restore and SQLite/LTX are unchanged.

## Verification

Run `hack/it components/diskio -p 1`. The recovery tests use actual HTTP clients,
uploads, snapshot compression, LBD parsing, and disk files against a simulated
cloud catalog. They restore into a fresh directory after deleting the covered
prefix, compare bytes including a retired-only block and tail writes/trims,
exercise more than 10,000 segments across 101 list pages, and reject incomplete/
corrupt history, changed page selections, and absent activation acknowledgements.
Mount/detach tests also run against a cloud without v1, for legacy and stream
volumes, and check a legacy volume's first checkpoint at detach.
The ext4/SQLite case also runs `e2fsck -fn`, extracts the database with `debugfs`,
checks `PRAGMA integrity_check`, and verifies rows across the checkpoint boundary.
This does not substitute for a joint test against MIR-2185's real catalog/GC or
for kernel LBD detach testing on an accelerator-enabled host.
