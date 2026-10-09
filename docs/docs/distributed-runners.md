---
title: Distributed Runners
description: Scale your cluster across multiple machines by adding runner nodes that host sandboxes alongside the coordinator.
keywords: [distributed runners, runner, coordinator, nodes, scaling, cluster, overlay network, scheduling]
---

import CliCommand from '@site/src/components/CliCommand';

# Distributed Runners

A Miren cluster starts as a single machine: one server that builds your apps, schedules them, and runs every sandbox. That's plenty to get going, but one machine has a ceiling. Eventually you run out of CPU and memory, and your only move is a bigger box.

Distributed runners are the way past that ceiling. You add more machines to the cluster, and Miren spreads your workloads across all of them, so you scale out instead of up.

## Minimum working example

Three commands take a cluster from one machine to two. On your workstation, mint a join token:

<CliCommand context="client">
```miren
miren runner token create
```
</CliCommand>

On the machine you're adding, join with that token and install the runner as a service:

<CliCommand context="server">
```miren
miren runner join mren_...
miren runner install
```
</CliCommand>

Once `miren runner list` shows the node ready, the scheduler starts placing sandboxes on it — nothing changes in your apps.

## How it works

A distributed cluster has two kinds of nodes: one **coordinator** and any number of **runners**.

The **coordinator** is the machine you started with, the one running `miren server`. It stays in charge of everything that has to live in one place: the entity store and its embedded etcd, the image registry, workload-identity signing, and the scheduler that decides where sandboxes run. The coordinator also runs your workloads itself, so a two-node cluster is really a coordinator plus one runner, not an idle controller plus one worker.

A **runner** is any other machine that has joined the cluster with `miren runner join`. Runners host sandboxes and nothing else: they pull the images they need, run your workloads, and report health back to the coordinator, but they hold no cluster state of their own. Every runner depends on the coordinator being reachable.

### Networking

Sandboxes across every node share a single flat overlay network. A sandbox on one runner can reach a sandbox on another by IP as if they were on the same host, so your services keep talking to each other the same way they did on a single machine. Miren handles the addressing, routes, and WireGuard peers, but your firewall must allow `51820/udp` between every pair of nodes.

:::info[Overlay address ranges]
Sandbox IPs are allocated from `10.8.0.0/16`, with each node leasing its own `/24` out of that range. Internal service addresses use `10.10.0.0/16`. Keep these ranges clear of your host and datacenter networks to avoid routing conflicts.
:::

### What runs where

The scheduler places each sandbox on a node when it starts. Two rules shape where things land:

- **Stateless workloads prefer runners.** Ordinary web and worker sandboxes are spread out across your runner nodes, keeping the coordinator free for the work only it can do. If no runners are available, they fall back to the coordinator.
- **Anything with a disk stays on the coordinator.** Disks, local storage, and host mounts are all node-local, so a sandbox that mounts one can't yet move between machines. Miren pins those sandboxes to the coordinator. See [Persistent Storage](./disks.md) for the details.

When an app runs several instances, the scheduler spreads them across nodes rather than stacking them on one, so losing a single machine doesn't take out your whole service.

### Images

Runners pull the images they need from the registry on the coordinator. There's no separate registry to run or configure, and no manual step to distribute an image to your runners. When you deploy, the coordinator resolves a configured image or builds one from source, and each runner that needs to start a sandbox pulls the selected image on demand.

## Adding a runner

Growing your cluster is three steps: mint a join token, join the new machine, and start it running.

### 1. Create a join token

From your workstation, create a token that authorizes a machine to join:

<CliCommand context="client">
```miren
miren runner token create
```
</CliCommand>

This prints an `mren_...` token with the coordinator's address baked in. Tokens are one-time by default and expire after an hour. To provision several machines from the same token, pass `--reusable`, and adjust the lifetime with `--ttl`. See [`runner token create`](./command/runner-token-create.md) for the full set of options.

:::warning[Treat join tokens like secrets]
A join token lets any machine enroll as a runner in your cluster. Don't commit it, log it, or paste it anywhere it might be captured. Prefer one-time tokens, and revoke anything unused with [`runner token revoke`](./command/runner-token-revoke.md).
:::

### 2. Join the new machine

On the machine you want to add, run `join` with the token. The token has the coordinator's address baked in, so first make sure this machine can reach the coordinator there (port 8443 by default). In multi-cloud or split-network setups, that path isn't automatic. You can pass the token as an argument, or pipe it in over stdin to keep it out of your shell history:

<CliCommand context="server">
```miren
miren runner join mren_...
```
</CliCommand>

Joining registers the machine as a runner, exchanges the token for a client certificate, and writes a config file to `/var/lib/miren/runner/config.yaml`. Each runner gets a stable identity; if you ever need to re-add a machine, remove the old entry first (see [caveats](#things-to-know) below). Reach for [`runner join`](./command/runner-join.md) for flags like `--name` and `--labels`.

Joining is only the first connection a runner makes. Once it's running it also
talks to the coordinator's etcd endpoint, so if there's a firewall between your
machines it needs to allow rather more than 8443. These are the defaults:

| Port | Protocol | What it carries | Protection |
|------|----------|-----------------|------------|
| 8443 | UDP | Coordinator API, including join and the metrics and logs a runner ships | Join token while enrolling, mTLS afterward |
| 12379 | TCP | etcd, for Flannel subnet coordination | mTLS |
| 51820 | UDP | WireGuard overlay | WireGuard encryption |

The coordinator API is QUIC, so 8443 is UDP rather than TCP. Opening the TCP
port instead is a common way to end up with a runner that can't join. The same
ports are listed in the [firewall reference](./firewall.md#between-nodes-distributed-runners).

Metrics and logs travel over 8443 alongside everything else a runner sends the
coordinator. VictoriaMetrics and VictoriaLogs themselves stay bound to loopback
on the coordinator and never need to be reachable from a runner, which is why
they aren't in that table. A runner authenticates each batch with a short-lived
identity token scoped to telemetry, on top of the certificate it got at join.

The overlay port needs to be open between runners, not just from each runner
to the coordinator, since sandboxes on different machines send traffic to each
other node-to-node. The coordinator's ports are configurable; see the
[server configuration reference](./server-config.md).

### 3. Start the runner

For a quick trial, start the runner in the foreground:

<CliCommand context="server">
```miren
miren runner start
```
</CliCommand>

For anything you want to keep around, install it as a systemd service instead. This downloads the runner bundle, sets up the service, and keeps the runner running across reboots:

<CliCommand context="server">
```miren
miren runner install
```
</CliCommand>

Back on your workstation, confirm the new node showed up and is healthy:

<CliCommand context="client">
```miren
miren runner list
```
</CliCommand>

Once the runner reports ready, the scheduler starts placing sandboxes on it. There's nothing to change in your apps.

## Automating enrollment

Joining machines by hand is fine for a node or two. But once you're provisioning runners from Terraform or an autoscaling group, spinning them up and down as load shifts, you don't want a human in the loop for each one. The pieces are already here: a reusable token plus `runner install` gives a fresh machine everything it needs to enroll itself on first boot.

Start with a **reusable** token instead of the one-time kind. Create it once and hand the same token to every machine you provision:

<CliCommand context="client">
```miren
miren runner token create --reusable --name infra --ttl 0
```
</CliCommand>

`--ttl 0` makes the token long-lived; set a real expiry like `--ttl 30d` if you'd rather rotate on a schedule. Store it wherever your infrastructure already keeps secrets.

:::danger[A reusable token is a standing key to your cluster]
Anyone holding it can enroll a runner, and it isn't consumed on use. Keep it in a secret manager, scope its TTL, and revoke it with [`runner token revoke`](./command/runner-token-revoke.md) the moment it's no longer needed or might have leaked.
:::

Then, in your machine's provisioning script (cloud-init, user data, an image build step), fetch the token and hand it to `runner install`. That single command downloads the runner, enrolls it, and sets up the systemd service, so the node comes up ready to take work:

```bash
# Skip if this machine already enrolled on a previous boot
if systemctl cat miren-runner.service >/dev/null 2>&1; then
  echo "Runner already enrolled"
else
  # Pull the reusable token from your secret store
  TOKEN=$(your-secret-tool read miren/runner-token)

  miren runner install \
    --token "$TOKEN" \
    --skip-system-check \
    --force
fi
```

The `systemctl cat` guard keeps this idempotent. The service file lives on the boot disk, so it survives reboots and the script does nothing on a second run, while a freshly recreated machine has no service yet and enrolls cleanly. `--skip-system-check` stops the non-interactive install from pausing on a requirements prompt, and `--force` overwrites the service file left by any half-finished earlier attempt, so a retried boot installs cleanly. Add `--name <name>` to give the runner a readable identity in [`runner list`](./command/runner-list.md), and `--branch <release>` to pin a specific runtime version.

This is how we run Miren's own fleet: a reusable enrollment token in a secret manager, a Terraform module that bakes the `runner install` call into each instance's startup, and scaling the fleet up or down is just changing an instance count.

## Operating your runners

Day-to-day fleet management happens through the `runner` subcommands. A quick tour of the ones you'll reach for most:

- **Check on the fleet.** [`runner list`](./command/runner-list.md) shows every registered node and its health; [`runner status`](./command/runner-status.md) reports a single runner's health and configuration.
- **Take a node out of rotation.** [`runner cordon`](./command/runner-cordon.md) marks a runner unschedulable so no new sandboxes land on it, while leaving what's already running in place. [`runner uncordon`](./command/runner-uncordon.md) puts it back in rotation.
- **Empty a node.** [`runner drain`](./command/runner-drain.md) cordons a runner and then evicts its sandboxes so the pool controllers rebuild that capacity elsewhere. Use it before taking a machine down for maintenance.
- **Retire a node.** [`runner remove`](./command/runner-remove.md) deregisters a node and cleans up after it. Drain first, since remove refuses a node with active work unless you force it.
- **Keep runners current.** [`runner upgrade`](./command/runner-upgrade.md) updates a runner's binary in place.
- **Rotate credentials.** [`runner reissue`](./command/runner-reissue.md) rotates a runner's certificate without a full re-join, as long as its current certificate is still valid.

A typical maintenance window looks like: drain the node, do your work, then uncordon it (or remove it if it's not coming back).

### Experimental host queries

Use [`runner query`](./command/runner-query.md) to send a query through the
coordinator to a runner by name, ID, or short ID. It observes live host activity
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

### Query reference and validation RPCs

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

:::warning[Upgrading to the internal-only registry]
When upgrading from a release that serves the registry on the coordinator's public address to one that serves it only over WireGuard, image pulls can briefly fail. Miren Cloud-managed upgrades update the coordinator first, then restart runners one at a time; each runner resumes pulling images after its upgrade. For manual upgrades, upgrade runner binaries first while the old coordinator still serves the registry, then upgrade the coordinator and restart the runners again so they learn its internal address.
:::

## Things to know

A few properties of distributed clusters are worth keeping in mind as you plan:

- **The coordinator is the hub.** It holds the cluster state, the image registry, and the identity signer, and every runner depends on it. Runners keep their existing sandboxes running if the coordinator briefly goes away, but scheduling, deploys, and image pulls all need it back. For now that makes the coordinator a single point of coordination, so give it your most reliable machine. It won't stay that way: the control plane is built to grow into a multi-node setup that can survive losing a coordinator, and finishing that work is on our roadmap.
- **Stateful apps don't distribute yet.** Anything with a disk is pinned to the coordinator, so today distributed runners add capacity for stateless web and worker workloads, not for your databases. Letting stateful workloads migrate between nodes is something we're actively working toward. See [Persistent Storage](./disks.md).
- **Workload identity is issued by the coordinator.** Sandboxes on runners get their identity tokens by way of the coordinator, so token issuance depends on it being reachable and on an issuer being configured. See [Workload Identity](./workload-identity.md).
- **Metrics and logs flow to the coordinator.** Runners ship their sandboxes' metrics and logs back to the coordinator's observability stack, so everything lands in one place regardless of which node a sandbox ran on.
- **Re-adding a machine needs a clean slate.** Runner identities are unique. If you're rebuilding a machine that was previously a runner, remove the old registration with [`runner remove`](./command/runner-remove.md) before you join it again.

## Next steps

- [`miren runner`](./command/runner.md) — the full command reference for managing runners
- [Application Scaling](./scaling.md) — how Miren scales instances within your cluster
- [Persistent Storage](./disks.md) — why disks pin apps to the coordinator
