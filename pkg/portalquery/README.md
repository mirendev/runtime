# Portal query engine

This is the query engine vendored from `github.com/lab47/portal/query` at
commit `09f7eaaea5b174cae1b56958405681bf89ea85e3`
(`v0.0.0-20261007004044-09f7eaaea5b1`). It includes the upstream source and tests,
without Portal's transport, authentication, or CLI. The package name remains
`query`; import it as:

```go
import query "miren.dev/runtime/pkg/portalquery"
```

`query.Revision` identifies this vendored language revision and its Miren
adaptations. Update the upstream hash when re-vendoring and bump the Miren suffix
for local changes to syntax, available fields, or query semantics. Runner query
RPCs return this revision even on parse/execution errors, so clients can detect
version skew with their local reference.

Local adaptations: external-package test imports and symbol-test function names
use the Miren import path. The documented rollup example and its parser test use
the local README and Miren CLI. The kernel loss-counter test filters its pinned
thread rather than all process threads so runtime syscalls cannot inflate the
expected count. PID-sensitive live eBPF tests require the host PID namespace,
since kernel events use host IDs, and are skipped in nested namespaces such as
iso. Process-symbol fixtures require matching procfs and filesystem executable
identities; overlay filesystems that expose different identities are skipped.
Tracepoint field offsets and sizes are parsed at their destination bit
widths to reject integer overflow before building eBPF instructions. Upstream
lint exceptions are scoped in `.golangci.yml` to preserve the other copied code.
The upstream repository has no `LICENSE` or `NOTICE`
file at this revision; no upstream licensing terms are inferred by this copy.

#### Rollups and computed columns

Use `rollup by` to keep a subset of a table's output group names, either when
emitting it or on either side of a join. This avoids collecting the source twice
just to get coarser groups. For example:

```sh
miren runner query node-a '
disk:completion where device_name = "nvme0n1" {
  @io[owner: io.cgroup.path, operation: operation] = {ops: count(), sectors: sum(sectors), p99: percentile(duration_ns, 99)}
}
cgroups {
  @kernel[owner: path] = {write_ops: rate(io.write_ios)}
}
after 30s {
  emit @io;
  emit @io rollup by owner full join @kernel on owner
    select ops_per_s = io.ops / window.seconds,
           kib_per_s = io.sectors * 512 / 1024 / window.seconds,
           ratio = ops_per_s / kernel.write_ops
    order by kib_per_s desc limit 20
}'
```

## Inventory-driven snapshot queries (agent reference)

Use `query.Engine` to correlate a registered custom inventory snapshot with a
built-in sampled source **before** collection and aggregation. This is a lookup,
not a join of completed aggregate tables. No inventory names or fields are built
into Portal.

## Syntax

```text
TARGET using (INVENTORY [where FIELD = VALUE [and ...]]) on TARGET_KEY = INVENTORY_KEY
  [where TARGET_FILTER = VALUE [and ...]]
  [FUNCTION(FIELD) [, FUNCTION(FIELD) ...] over WINDOW [every INTERVAL] [by FIELD, ...]]
```

Inventory predicates use the existing custom-source equality and membership
syntax (`app = "my-app"`, `state in (running, starting)`). Custom snapshot
collectors must apply `MonitorRequest.Filters`, as for ordinary custom snapshots.
The clause precedes the target's own `where` and action/aggregate. `on` accepts
one exact string key, not numeric coercion, glob matching, composite keys,
expressions, subqueries, or references to completed aggregate tables.

Targets are built-in snapshot sources with a string identity field:

| Target | Target key |
| --- | --- |
| `cgroups` | `path` (not `id`) |
| `network`, `cpu`, `sensors` | `name` |
| `containers` | `id` |
| `gpu` | `uuid` |

The inventory can be any engine-local registered source with `Snapshots`.
It need not have `Events` or support sampled aggregation. Nested `using`, event
targets, aggregate inventories, and additional `cgroups where path = ...`
filters are rejected. Non-cgroup target filters retain their normal semantics.

## App I/O: exact examples for Miren

Given a registered `sandboxes` source whose flat rows contain `app`, `cgroup`,
`container_id`, `sandbox_id`, `version`, `pid`, and `state`:

```text
cgroups using (sandboxes where app = "my-app") on path = cgroup
  where result.format = rows
  rate(io.write_bytes), rate(io.write_ios) over 10s every 1s by inventory.app
```

This returns **total charged bytes/s and operations/s for the selected app's
containers**, not their mean, and not lifetime counters divided by the window.
Read I/O uses `rate(io.read_bytes)` / `rate(io.read_ios)`; discard I/O uses
`rate(io.discard_bytes)` / `rate(io.discard_ios)`. There are at most 8 metrics.

Selector/block equivalent, with named output columns and app alias:

```text
cgroups using (sandboxes where app = "my-app") on path = cgroup {
  @io[app: inventory.app] = {
    write_bytes_per_second: rate(io.write_bytes),
    write_ios_per_second: rate(io.write_ios)
  }
}
after 10s { emit @io }
```

Block syntax uses the default 1s sample interval. `after` returns one finite
result; periodic reporting blocks are still event-only. Correlated selectors can
also participate in ordinary multi-selector scripts and completed-table reports.

To inspect **current lifetime counters per cgroup** without sampling:

```text
cgroups using (sandboxes where app = "my-app") on path = cgroup
```

Snapshot results use `Snapshot.Data` / JSON `data`, an array of flat objects with
target fields (`path`, `id`, `io.write_bytes`, `io.write_ios`, etc.) and inventory
fields qualified as `inventory.app`, `inventory.container_id`, etc. Dot-containing
keys are literal flat keys; for jq use `.data[] | .["io.write_bytes"]`, not
`.data[].io.write_bytes`. Missing controller metrics remain absent, not zero.
For this correlated snapshot there is no separate `cgroups` array.

Aggregates retain existing result shapes: compact syntax above uses
`Snapshot.Aggregation.Rows` / JSON `aggregation.rows`; each row has a `group`
object and metric `values` array in expression order. Block syntax uses named
metric values. Inventory fields are dimensions for grouping/distinct count, not
numeric metrics. Add `path` or `inventory.container_id` to `by` for per-container
results. Use `rate(counter)` for app totals: `avg(counter_per_second)` computes
an average of container observations, while `sum(counter_per_second)` sums over
time as well as containers. Neither is an app's total window-average rate.
Raw cumulative counters cannot be passed to `sum`/`avg`.

## Execution and finite-window semantics

1. Validate the complete request and bind its registered inventory source.
2. Collect the filtered inventory **once per correlated selector** and freeze its
   rows for that selector's window. Multi-selector scripts resolve each selector's
   inventory independently, not as an atomic shared snapshot.
3. Match inventory keys to target identities exactly. For cgroups, collect each
   unique selected path separately; never collect all cgroups then filter an
   already-aggregated total. Exact collection prunes other branches/descendants.
   Other built-ins collect their normally filtered snapshot and then match rows.
4. Carry the frozen inventory fields onto matched target rows and apply the
   existing sampler/reducer, preserving target identity (cgroups: path + directory
   ID), not app name, as the counter-baseline key.

The aggregation window is `[start, start + WINDOW)`, maximum 1h, and includes
inventory collection time. After inventory completes, the first target snapshot
is collected immediately if time remains. Later snapshots follow the original
window's interval grid; slow reads skip ticks, never overlap or catch up in bursts.
Default interval is 1s, minimum 100ms; derived rates require `WINDOW > INTERVAL`.
Reads finishing at or beyond the window end are discarded. There is no mandatory
end-boundary read. A window too short for two observations cannot yield a rate.

`rate(counter)` sums valid per-target deltas and divides by the union of their
observed intervals within each output group. With concurrent container samples,
this is the sum of container rates, not their mean. Gaps are not charged as zero
and unobserved intervals are not extrapolated to the full window. Targets are
read serially, not atomically; a sample's timestamp is collection completion.

New inventory rows appearing during the window are not discovered. Stopped rows
remain selected: absent cgroups contribute no sample. Reappearing paths require
fresh baselines after a missing observation; recreated directories have new IDs
and cannot bridge lifetimes. Counter resets, missing controller/device counters,
or changed device sets suppress the affected interval's rate. Lifetime snapshot
counters include activity before the query, including descendants; they are not
the bytes written during the observation window. Frozen inventory is attribution
at query start, not a guarantee that a reused path still belongs to the same app.

No matches means an empty snapshot or grouped aggregate, not a synthetic zero
app row. Ungrouped aggregates retain their existing empty-window defaults (e.g.
count is 0 and rate is null). Missing/empty/null inventory keys are ignored.
An inventory collector that returns a deadline error
at the window end yields an empty aggregate; caller cancellation/deadline errors
propagate, never a partial success. Other inventory or target errors fail the
entire query. Collectors must honor the supplied context; the engine does not
spawn detached goroutines to interrupt a collector that ignores cancellation.
A non-aggregate snapshot has only its caller's deadline, not an implicit window.

## Multiplicity, overlap, and collection limits

- Multiple disjoint cgroups for one app all contribute, including pause containers
  if the inventory selects them. App filtering does not infer container roles.
- Identical inventory rows with the same target key are deduplicated. Different
  rows sharing a target key fail as ambiguous ownership, even if only an unused
  inventory field differs. Filter or deduplicate inventory rather than multiply
  target metrics. Duplicate physical target identities also fail.
- Selecting a cgroup and any descendant **fails** rather than summing overlapping
  hierarchical accounting. This also applies across different selected apps.
  Choose disjoint workload roots; the engine neither subtracts child counters
  nor silently discards either owner. A parent alone includes its descendants.
- Cgroup keys must be canonical absolute paths relative to the visible cgroup v2
  mount (e.g. `/apps/worker`), not host filesystem paths or globs. Symlink aliases,
  `..`, repeated/trailing slashes, and glob metacharacters are not selectors.
- Inventory data must be a JSON array of flat objects, containing only declared
  scalar fields (strings, numbers, booleans, null); the matching key must be a
  string. A typed Go slice of structs with flat JSON tags works too. Return `[]`
  for an empty inventory, not `null`; null rows are also rejected.
- At most 4096 inventory rows **before deduplication**, 7 MiB serialized inventory,
  4096 target records per collected snapshot, and 4096 matched records per tick.
  Up to 4096 distinct cgroup paths are read serially each tick. Narrow inventory
  filters for large apps; slow collection reduces sampling coverage.
- Existing reducer limits remain: 4096 retained metric groups, 65536 retained
  values, at most 4 grouping fields, and 4096 bytes of query text. Remote output
  retains the transport's 7 MiB limit. Limits fail, not silently truncate; a result
  limit affects presentation, not collection or ownership validation.

## Go integration

Register inventory before using the engine, then use `engine.ParseMonitorQuery`
(not package-level `query.ParseMonitorQuery`, which cannot know custom sources)
and `engine.Query(ctx, request)`. The serialized API equivalent is:

```go
request := query.MonitorRequest{
    Source: "cgroups", Mode: "aggregate",
    Using: &query.SnapshotCorrelation{
        Inventory: query.MonitorRequest{
            Source: "sandboxes", Mode: "snapshot",
            Filters: []query.SourceFilter{{Field: "app", Values: []string{"my-app"}}},
        },
        Field: "path", Key: "cgroup",
    },
    Aggregation: &query.AggregationRequest{
        Function: "rate", Field: "io.write_bytes",
        Window: 10*time.Second, Every: time.Second,
        GroupBy: []string{"inventory.app"}, Compact: true,
    },
}
result, err := engine.Query(ctx, request)
```

`Engine.Validate` and `Engine.Metadata` rebind inventory registrations after JSON
transport. Inventory fields appear in grouping metadata. Engines lacking the
registration reject the request. Update Portal on both sides of any request
transport to retain the new `using` field. Miren uses this vendored engine with
the new expression/result decoding, **not** an event implementation or a sampler
for `sandboxes`. Its existing snapshot collector must honor filters,
return the declared flat fields, and use canonical cgroup paths. Portal does not
add Miren-specific authorization or inventory discovery.
