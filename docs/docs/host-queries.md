---
title: Host Queries
description: Run short, bounded observations of live host activity on any node in your cluster with miren runner query.
keywords: [runner query, host query, cgroup, cpu, memory, network, observability, nodes]
---

import CliCommand from '@site/src/components/CliCommand';

# Host Queries

`miren runner query` takes a short, bounded look at what a node's host is doing right now: CPU, memory, cgroup I/O, network, and more, optionally scoped to one app's sandboxes. It works on any node in your cluster, including the server itself on a single-machine cluster, and it's limited to cluster operators.

:::warning[Experimental]
Host queries are experimental, so expect the query syntax and output to change.
:::

## Running a query

Use [`runner query`](./command/runner-query.md) to send a query through the
coordinator to a node by name, ID, or short ID, as `miren runner list` shows them. It observes live host activity
right now or during the query window, never historical data:

<CliCommand context="client">
```miren
miren runner query --reference
miren runner query runner1 memory
miren runner query runner1 "memory avg(used) over 10s every 1s"
```
</CliCommand>

`--reference` prints the full supported query reference entirely offline; it
needs no runner argument, cluster connection, or credentials. It includes compact
and selector/action syntax, source filters and fields, sampling semantics, joins,
rollups, computed columns, stacks, JSON result shapes, limits, and validated
examples. Built-in field names, types, units, and counter/gauge semantics come
directly from the vendored engine's metadata. No external reference is needed.

The command prints the live snapshot or aggregate as JSON, suitable for piping
to `jq`. Query failures print an error and exit nonzero without a result on
stdout. Quote expressions containing spaces so the shell passes them as one
argument.

The coordinator's `dev.miren.runtime/runner` RPC service exposes
`RunnerRegistration.Query(runner, expression)`. It resolves a runner by name,
runner ID, entity ID, or short ID, forwards the expression to that runner, and
returns its name and a JSON-encoded result in `data` (bytes). Failures
are reported in `error`; transport failures can also fail the RPC call.

Results also expose `engine_revision`, identifying the **target runner's**
query language, including when parsing or execution fails. The coordinator
forwards it unchanged, not its own revision. Older runners omit this field;
lookup and connection failures have no runner revision either. Clients must
treat a missing revision as unknown, not as a match.

The offline `--reference` starts with the CLI's engine revision. CLI query errors
include the runner's revision when available and explicitly flag differences.
If revisions differ, a syntax error may indicate version skew rather than a
mistake against the offline reference: fetch the target runner's reference using
`RunnerRegistration.QueryInfo(runner)`. The expression stays a string on the wire
so unsupported syntax fails loudly instead of silently dropping newer request fields.

## Query reference and validation RPCs

The same coordinator service exposes two non-executing APIs:

- `RunnerRegistration.QueryInfo(runner)` returns `name`, `engine_revision`, and
  the complete, self-contained syntax guide in `reference`. It comes from the
  **target runner**, not the coordinator, and includes the same guide available
  offline through `miren runner query --reference` on a matching CLI version.
- `RunnerRegistration.ValidateQuery(runner, expression)` returns `name`,
  `engine_revision`, `valid`, and `error`. It parses and checks the expression
  against the runner's registered sources, including custom source fields,
  aggregates, selector/action blocks, and inventory correlation. Symbolic syscall
  names are resolved against that runner's native architecture.

Validation does **not** run collectors, attach probes, read sandbox inventory,
or reserve one of the ten execution slots. It does not check runtime prerequisites
such as kernel probe support, permissions, existing processes/cgroups, inventory
ownership conflicts, or whether the query completes within Miren's one-minute
execution deadline. `valid = true` means the expression is accepted by the
runner's language, not that executing it will succeed.

Both APIs use the same runner lookup and operator identity restrictions as
`Query`. Cloud JWT authorization requires `runnerregistration/queryinfo` or
`runnerregistration/validatequery`, respectively. Check both transport errors
and the returned `error`; lookup, forwarding, or authorization failure is not a
syntax diagnosis. Older runners/coordinators may not implement these methods;
a missing engine revision is unknown, not a match.

```go
client := runner_v1alpha.NewRunnerRegistrationClient(cl)
info, err := client.QueryInfo(ctx, "runner1")
if err != nil {
    return err
}
if info.Error() != "" {
    return fmt.Errorf("query reference failed: %s", info.Error())
}
fmt.Print(info.Reference()) // Full guide, suitable for an agent to read directly.

validation, err := client.ValidateQuery(ctx, "runner1", "memory avg(used) over 10s every 1s")
if err != nil {
    return err
}
if validation.Error() != "" {
    return fmt.Errorf("query validation failed: %s", validation.Error())
}
if !validation.Valid() {
    return fmt.Errorf("runner did not accept the query")
}
// No query has executed. Use client.Query to collect its results.
```

Expressions use the live query DSL, not SQL. Use
`miren runner query --reference` for the complete syntax. For example:

```text
memory
network where name = lo
process where name = worker*
memory avg(used) over 10s every 1s
```

Miren adds two containerd-backed sources:

- `sandboxes` is a snapshot of the containers currently known to the runtime,
  with `sandbox_id`, `container_id`, `app`, `version`, numeric `pid`, `state`,
  and `cgroup` fields.
- `sandbox_events` is an event/aggregation-only source with `sandbox_id`,
  `container_id`, `app`, `version`, `action` (`start`, `exit`, or `oom`), numeric
  `pid`, and numeric `exit_status` fields.

Inventory has one row per labeled Miren container, including sandbox pause
containers. `state` is the containerd task state, or `no_task` when no task
exists. Snapshot filters support equality and membership, not numeric
inequalities or sampled aggregation. Event `pid` is present on starts/exits;
`exit_status` is present only on exits. Exec-process exits are excluded.
App names are best-effort enrichment from version metadata and may be empty.
Events whose container metadata has already disappeared cannot be attributed
and are omitted. These sources never return container environment variables.

These sources reflect containerd runtime truth, not application readiness.
Snapshots describe the current state and event aggregations cover only their
current query window; neither source is historical storage. `runner query` is a
finite request/response command, so snapshots and bounded aggregates are
supported, but unbounded event streaming is not.

Use inventory correlation to discover an app's cgroups and measure its disk I/O
in a **single query**, without a separate client-side cgroup lookup:

```bash
miren runner query runner1 'cgroups using (sandboxes where app = "my-app" and state = running) on path = cgroup where result.format = rows rate(io.write_bytes), rate(io.write_ios) over 10s every 1s by inventory.app'
```

The result contains app-total bytes/second and operations/second in
`aggregation.rows`, with values aligned to `columns`. Remove the aggregate
suffix to inspect lifetime counters: correlated snapshots use a flat `data`
array with literal keys such as `io.write_bytes` and `inventory.app`, not a
`cgroups` array. Selector/action blocks support the same `using (...) on ...`
clause; `--reference` includes the full syntax and examples.

:::warning[Inventory attribution is frozen for the query]
Inventory is resolved once per selector; new sandboxes are not discovered during
the window. Missing or recreated cgroups need fresh counter baselines. Identical
inventory rows deduplicate, while ambiguous owners and overlapping parent/child
cgroups fail rather than double-counting I/O. Choose disjoint workload cgroups.
:::

:::warning[Queries inspect the runner host]
Queries run with the runner daemon's host visibility and privileges, not inside
an app sandbox. The coordinator applies its normal RPC authentication and
authorization. Cloud JWT operators need `runnerregistration/query` permission.
Local certificate callers must use `miren-user`, `miren-server`, or `miren-api`;
runner/service certificates and custom certificate names cannot use this RPC.
Explicit auth-disabled mode remains open. The runner accepts host queries only
from the coordinator's cluster certificate. eBPF-backed sources require kernel
support and appropriate host privileges.
:::

Using the generated Go client with an authenticated coordinator connection:

```go
cl, err := state.Connect(coordinatorAddress, rpc.ServiceRunner)
if err != nil {
    return err
}
defer cl.Close()

result, err := runner_v1alpha.NewRunnerRegistrationClient(cl).Query(ctx, "runner1", "memory")
if err != nil {
    return err
}
if result.Error() != "" {
    return fmt.Errorf("host query failed: %s", result.Error())
}
// result.Data() contains the live JSON snapshot or aggregate.
```

Execution has a one-minute deadline and also respects caller cancellation.
Each runner executes at most 10 queries at a time. Additional calls fail with
`runner already has 10 queries in progress` rather than queueing. Admission is
released on success, error, or cancellation.
This is a single-runner, request/response API, not a streaming or
cluster-wide query service. Both coordinator and runner must support this RPC.
