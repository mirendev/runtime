---
title: Cloud-Routed Clusters
description: Reach a cluster through Miren Cloud when your machine has no route to it, using the same credentials and the same permissions as a direct connection.
keywords: [cloud routed, via cloud, relay, unreachable cluster, firewall, nat, rpc]
---

# Cloud-Routed Clusters

Normally the `miren` CLI dials your cluster directly. That needs a route to it: a
public address, a VPN, or a tunnel. Plenty of clusters have none. A cluster
behind office NAT, on a home network, or inside a private subnet is perfectly
healthy and still unreachable from wherever you happen to be sitting.

Those clusters already hold an outbound connection to Miren Cloud, which is how
they report status and receive work. A cloud-routed cluster reuses that
connection in the other direction: the CLI connects to cloud, cloud passes the
traffic down the link the cluster already opened, and the cluster answers.

```
miren  →  Miren Cloud  →  the cluster's existing link  →  your cluster
```

Nothing new is opened on the cluster side, so there is no port to forward and no
firewall rule to add.

## Setting one up

Usually you do not have to do anything. `miren cluster add` picks this route on
its own when it is the one that works:

```bash
miren cluster add
```

If the cluster you pick advertises no address your machine can dial, or
advertises addresses that do not answer, the command tries the cloud route
itself: it opens a session through cloud and makes a call. If the cluster
answers, the entry is written to route through cloud and it tells you so. A
cluster that neither you nor cloud can reach still fails, because that is a real
problem rather than a routing choice.

It tries the route rather than asking whether one exists, because those are
different questions. Cloud can hold a link to a cluster whose runtime is older
than this feature, and that cluster will sit there without answering. Making the
call is what tells the two apart.

To skip the direct attempt entirely, ask for it:

```bash
miren cluster add --via-cloud
```

Either way you need to be logged in first (`miren login`), and the cluster needs
to be registered with cloud and online.

After that, use it like any other cluster:

```bash
miren cluster use my-cluster
miren app list
miren deploy
```

Deploys work over this route, including the build-context upload.

## What it does not change

**Your permissions are unchanged.** Cloud decides whether it will carry your
traffic at all, based on your membership of the organization that owns the
cluster. What you may actually *do* is decided by the cluster itself, from your
credential, against its own RBAC policy — exactly as when you connect directly.
Being able to reach a cluster this way does not grant you anything on it.

**Cloud does not read your traffic.** The frames it relays are the Miren RPC
protocol, which cloud passes along without interpreting. Your credential travels
inside them and is checked by the cluster.

**Audit still names you.** Calls arriving this way are attributed to you, not to
cloud.

## How cloud authorization stays current

For cloud-authenticated callers, the runtime validates your JWT to establish
your identity, then authorizes locally using the rules and group
memberships pushed over its cluster connection. Group claims in an older token
do not grant access. Policy edits and membership changes are pushed without
waiting for a polling interval or a new login.

Each snapshot contains all current users in the cluster's organization, including
users with no effective groups, and their explicit and implicit default group
memberships. It also includes active same-organization service accounts (`svc-*`
principals), including those with no groups, with their explicit same-organization
groups only; user defaults do not apply to service accounts. Suspended, revoked,
and deleted service accounts are omitted. Principals absent from the snapshot are
denied. The authenticated cluster session scopes this map to its organization;
JWT organization claims are not used for scope (user tokens omit them, and
service-account tokens contain numeric database IDs rather than organization
XIDs). Cloud sends the organization's rules with their tag selectors; the runtime still evaluates those
selectors against its own cluster tags. Policy and memberships are replaced
together, and cached grants are invalidated on every update.

JWT authorization is denied before the first snapshot, while the cluster
connection is disconnected, and after reconnect until a fresh snapshot arrives.
This applies to direct connections as well as cloud-routed ones, preventing
removed users or groups from retaining access during an outage. Local,
CA-verified client certificate access remains available and bypasses cloud RBAC.

:::warning[Upgrade cloud first]

A cloud that does not negotiate authorization version 1 cannot authorize JWT
callers on this runtime; update cloud before upgrading the runtime.

:::

After reconnecting, the cluster receives current permissions, including changes
made while offline, before JWT access resumes.

`miren debug rbac` and `miren debug rbac test` still perform an explicit, one-shot
HTTP policy fetch for troubleshooting. They do not inspect the running cluster's
snapshot or resolve a user's current groups.

Authentication diagnostics may still display group claims from the token. Those
claims describe when it was issued, not the effective groups used to authorize
the current request.

## Limits worth knowing

**A dropped link ends in-flight commands.** Sessions live on the cluster's
connection to cloud. If that connection drops, anything in flight fails and the
cluster reconnects on its own schedule, which can take up to a minute. A long
deploy that spans an outage will fail and need re-running. When this happens the
error says so — if you see `the cluster's link to the cloud dropped`, retry
rather than going looking for a fault.

**It is slower than a direct connection.** Every frame takes an extra hop, and
the relay is not the path to choose when you have a direct one available.

**One cloud, one cluster.** The route is per-cluster. Clusters you can reach
directly should stay that way.

## Configuration

`miren cluster add --via-cloud` writes this for you; the fields are documented
here because a hand-written config is sometimes easier to reason about.

```yaml
clusters:
  my-cluster:
    via_cloud: true
    xid: cluster-abc123        # the cluster's ID in cloud
    identity: cloud            # which login to authenticate with
```

The cloud used is the one your identity logged into. `cloud_url` overrides that,
which is what makes a cluster registered with one cloud reachable through
another:

```yaml
    cloud_url: https://api.miren.cloud
```

A cloud-routed cluster needs no `address` and no `ca_cert`: it is never dialed,
and the certificate on the wire belongs to cloud.

### Development clouds

A cloud reached over plain `http://` is refused unless it is on this machine,
because everything about the connection is your credential and an unencrypted
hop puts all of it on the wire. To reach a development cloud by hostname, say so
explicitly:

```yaml
    cloud_url: http://miren.host:3001
    insecure: true
```

## Troubleshooting

**`cluster not connected`** — cloud has no live link to the cluster. Check the
[Connectivity](./connectivity.md) panel; the cluster is offline or has lost its
uplink.

**`access denied by RBAC policy`** — you reached the cluster and it refused the
command. That is the cluster's own policy, not the relay. Check your current
organization membership and permissions. A newly granted permission becomes
usable when the corresponding push arrives.

**`cloud authorization is not synchronized`** — the cluster has no current
authorization snapshot, either because its cloud connection is down or because
it is waiting for a fresh snapshot. Check the [Connectivity](./connectivity.md)
panel and that cloud was upgraded first; JWT access resumes after synchronization.

**`the cluster's link to the cloud dropped`** — the cluster disconnected while
your command was running. Retry it.
