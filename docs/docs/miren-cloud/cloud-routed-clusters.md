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

Cloud defines access, so the cluster keeps authorizing from the last snapshot
cloud sent until cloud sends a new one. Losing the cluster connection,
reconnecting, and restarting the server don't change who can do what: the
cluster saves each snapshot to disk, along with cloud's token signing keys, and
picks up where it left off. An update the cluster can't read is ignored and
logged as an error, leaving the previous snapshot in place. This applies to
direct connections as well as cloud-routed ones.

JWT authorization is denied only before a cluster has ever received a snapshot,
or after it is registered to a different organization. Local, CA-verified client
certificate access is always available and bypasses cloud RBAC.

:::warning[Revocations wait for the cluster to hear from cloud]

While a cluster can't reach cloud, it keeps trusting what cloud last told it,
for as long as the outage lasts. That covers two kinds of revocation:

- **Permissions.** Removing someone from a group or from the organization
  reaches the cluster with its next snapshot. Until then, they keep the access
  they had.
- **Signing keys.** If cloud withdraws a token signing key, for example because
  it leaked, the cluster keeps accepting tokens signed with that key until it
  fetches cloud's current keys.

Restoring the connection delivers both. Where a cluster is reachable directly,
treat a long uplink outage as a window in which neither kind of revocation
applies there.

:::

:::warning[Upgrade cloud first]

A cloud that does not negotiate authorization version 1 cannot authorize
JWT callers on this runtime; update cloud before upgrading the runtime.

:::

The runtime currently offers only authorization version 1, retaining the existing
RPC-interface/method policy vocabulary. Version 2 negotiation is held for a
separate, approved Cloud/runtime rollout; it is not a CLI version. The runtime
retains version 2 enforcement for saved snapshots. This negotiation hold is not
a kill switch: a saved version 2 snapshot remains authoritative across restarts
until a valid replacement arrives, including a version 1 replacement.

Version 2 snapshots must explicitly include `version: 2` and use
logical permissions for migrated app operations (`app:read`, `app:create`,
`app:configure`, `app:deploy`, `app:destroy`, `app:logs`, and `app:exec`). The
runtime resolves the app's stored name and creator before evaluating its scope;
ownership alone never grants access without a matching policy rule. Creating an
app under version 2 requires an unscoped create grant, including implicit
creation during a build. A deploy grant alone is not a create grant.

Version 1 and standalone certificate, OIDC, and workload callers retain their
existing method permissions and app bindings, including implicit creation during
an authorized first build. Source-only analysis uses `builder:analyzeapp`, not
`app:read`, in both policy versions. Prepared uploads require the same initiating
subject only under logical Cloud policy. Existing sandbox app-confinement checks
remain in place; the additional authoritative owner lookup applies to logical
Cloud policy only.

Cloud must only emit scoped, logical permissions after negotiating version 2
with that specific runtime. Older runtimes ignore unknown permission fields,
so sending them a scoped rule can silently turn it into a cluster-wide grant.
Never strip scopes to downgrade a policy; version 1 grants must be genuinely
unscoped. Cloud must persist and emit scopes before version 2 can be enabled.
Upgrading the evaluator alone is not sufficient rollout protection. Version 1
and version 2 policies must not be combined as alternative grants: a broad legacy
grant must not override a scoped logical permission. Version selection and
policy replacement are atomic, and saved snapshots retain their vocabulary
across restarts. Before downgrading to a runtime without version 2 support,
replace the saved authorization state and policy with an appropriate version 1
policy; old binaries cannot safely read version 2 snapshots.

Logical authorization is being migrated incrementally. Collection filtering,
related-resource authorization, raw entity/infrastructure gates, ownership
transfer, and standalone file policies are separate changes. Do not enable a
restricted shared-cluster policy until those enforcement paths are deployed.

After reconnecting, the cluster receives current permissions, including changes
made while it was offline, and applies them as soon as they arrive.

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

**`unauthorized: denied by RBAC policy`** — you reached the cluster and it refused the
command. That is the cluster's own policy, not the relay. Check your current
organization membership and permissions. A newly granted permission becomes
usable when the corresponding push arrives.

**`cloud authorization is not synchronized`** — the cluster has never received
an authorization snapshot from its current organization, usually because it was
just registered or upgraded and hasn't connected to cloud since. Check the
[Connectivity](./connectivity.md) panel. JWT access starts once the first
snapshot arrives.

**`the cluster's link to the cloud dropped`** — the cluster disconnected while
your command was running. Retry it.
