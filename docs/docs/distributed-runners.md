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
- **Anything with a disk stays on the coordinator.** Disks, local storage, and host mounts are all node-local, so a sandbox that mounts one can't yet move between machines. Miren pins those sandboxes to the coordinator. See [Persistent Storage](/disks) for the details.

When an app runs several instances, the scheduler spreads them across nodes rather than stacking them on one, so losing a single machine doesn't take out your whole service.

### Images

Runners pull the images they need from the registry on the coordinator. There's no separate registry to run or configure, and no manual step to push a build out to your runners. When you deploy, the coordinator builds the image once, and each runner that needs to start a sandbox pulls it on demand.

## Adding a runner

Growing your cluster is three steps: mint a join token, join the new machine, and start it running.

### 1. Create a join token

From your workstation, create a token that authorizes a machine to join:

<CliCommand context="client">
```miren
miren runner token create
```
</CliCommand>

This prints an `mren_...` token with the coordinator's address baked in. Tokens are one-time by default and expire after an hour. To provision several machines from the same token, pass `--reusable`, and adjust the lifetime with `--ttl`. See [`runner token create`](/command/runner-token-create) for the full set of options.

:::warning[Treat join tokens like secrets]
A join token lets any machine enroll as a runner in your cluster. Don't commit it, log it, or paste it anywhere it might be captured. Prefer one-time tokens, and revoke anything unused with [`runner token revoke`](/command/runner-token-revoke).
:::

### 2. Join the new machine

On the machine you want to add, run `join` with the token. The token has the coordinator's address baked in, so first make sure this machine can reach the coordinator there (port 8443 by default). In multi-cloud or split-network setups, that path isn't automatic. You can pass the token as an argument, or pipe it in over stdin to keep it out of your shell history:

<CliCommand context="server">
```miren
miren runner join mren_...
```
</CliCommand>

Joining registers the machine as a runner, exchanges the token for a client certificate, and writes a config file to `/var/lib/miren/runner/config.yaml`. Each runner gets a stable identity; if you ever need to re-add a machine, remove the old entry first (see [caveats](#things-to-know) below). Reach for [`runner join`](/command/runner-join) for flags like `--name` and `--labels`.

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
ports are listed in the [firewall reference](/firewall#between-nodes-distributed-runners).

Metrics and logs travel over 8443 alongside everything else a runner sends the
coordinator. VictoriaMetrics and VictoriaLogs themselves stay bound to loopback
on the coordinator and never need to be reachable from a runner, which is why
they aren't in that table. A runner authenticates each batch with a short-lived
identity token scoped to telemetry, on top of the certificate it got at join.

The overlay port needs to be open between runners, not just from each runner
to the coordinator, since sandboxes on different machines send traffic to each
other node-to-node. The coordinator's ports are configurable; see the
[server configuration reference](/server-config).

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
Anyone holding it can enroll a runner, and it isn't consumed on use. Keep it in a secret manager, scope its TTL, and revoke it with [`runner token revoke`](/command/runner-token-revoke) the moment it's no longer needed or might have leaked.
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

The `systemctl cat` guard keeps this idempotent. The service file lives on the boot disk, so it survives reboots and the script does nothing on a second run, while a freshly recreated machine has no service yet and enrolls cleanly. `--skip-system-check` stops the non-interactive install from pausing on a requirements prompt, and `--force` overwrites the service file left by any half-finished earlier attempt, so a retried boot installs cleanly. Add `--name <name>` to give the runner a readable identity in [`runner list`](/command/runner-list), and `--branch <release>` to pin a specific runtime version.

This is how we run Miren's own fleet: a reusable enrollment token in a secret manager, a Terraform module that bakes the `runner install` call into each instance's startup, and scaling the fleet up or down is just changing an instance count.

## Operating your runners

Day-to-day fleet management happens through the `runner` subcommands. A quick tour of the ones you'll reach for most:

- **Check on the fleet.** [`runner list`](/command/runner-list) shows every registered node and its health; [`runner status`](/command/runner-status) reports a single runner's health and configuration.
- **Take a node out of rotation.** [`runner cordon`](/command/runner-cordon) marks a runner unschedulable so no new sandboxes land on it, while leaving what's already running in place. [`runner uncordon`](/command/runner-uncordon) puts it back in rotation.
- **Empty a node.** [`runner drain`](/command/runner-drain) cordons a runner and then evicts its sandboxes so the pool controllers rebuild that capacity elsewhere. Use it before taking a machine down for maintenance.
- **Retire a node.** [`runner remove`](/command/runner-remove) deregisters a node and cleans up after it. Drain first, since remove refuses a node with active work unless you force it.
- **Keep runners current.** [`runner upgrade`](/command/runner-upgrade) updates a runner's binary in place.
- **Rotate credentials.** [`runner reissue`](/command/runner-reissue) rotates a runner's certificate without a full re-join, as long as its current certificate is still valid.

A typical maintenance window looks like: drain the node, do your work, then uncordon it (or remove it if it's not coming back).

## Things to know

A few properties of distributed clusters are worth keeping in mind as you plan:

- **The coordinator is the hub.** It holds the cluster state, the image registry, and the identity signer, and every runner depends on it. Runners keep their existing sandboxes running if the coordinator briefly goes away, but scheduling, deploys, and image pulls all need it back. For now that makes the coordinator a single point of coordination, so give it your most reliable machine. It won't stay that way: the control plane is built to grow into a multi-node setup that can survive losing a coordinator, and finishing that work is on our roadmap.
- **Stateful apps don't distribute yet.** Anything with a disk is pinned to the coordinator, so today distributed runners add capacity for stateless web and worker workloads, not for your databases. Letting stateful workloads migrate between nodes is something we're actively working toward. See [Persistent Storage](/disks).
- **Workload identity is issued by the coordinator.** Sandboxes on runners get their identity tokens by way of the coordinator, so token issuance depends on it being reachable and on an issuer being configured. See [Workload Identity](/workload-identity).
- **Metrics and logs flow to the coordinator.** Runners ship their sandboxes' metrics and logs back to the coordinator's observability stack, so everything lands in one place regardless of which node a sandbox ran on.
- **Re-adding a machine needs a clean slate.** Runner identities are unique. If you're rebuilding a machine that was previously a runner, remove the old registration with [`runner remove`](/command/runner-remove) before you join it again.

## Next steps

- [`miren runner`](/command/runner) — the full command reference for managing runners
- [Application Scaling](/scaling) — how Miren scales instances within your cluster
- [Persistent Storage](/disks) — why disks pin apps to the coordinator
