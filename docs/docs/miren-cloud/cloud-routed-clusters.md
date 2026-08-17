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

```bash
miren cluster add --via-cloud
```

With no address to dial, the command looks your clusters up in cloud and asks
which one you mean. You need to be logged in first (`miren login`), and the
cluster needs to be registered with cloud and online.

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
command. That is the cluster's own policy, not the relay. A permission granted a
moment ago can take a short while to take effect.

**`the cluster's link to the cloud dropped`** — the cluster disconnected while
your command was running. Retry it.
