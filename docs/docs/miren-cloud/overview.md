---
title: Miren Cloud
description: Give your self-hosted Miren clusters a public front door, team access, and multi-environment workflows while you keep the infrastructure.
keywords: [miren cloud, control plane, subdomains, miren anywhere, teams, multi-environment]
---

# Miren Cloud

You install Miren on your own server and you own the whole thing. Run it standalone and it works great on its own, but it's an island: your apps are only reachable where your network reaches, backups are yours to arrange, and it's just you managing it.

Miren Cloud is the connective tissue you can opt into. It doesn't take over your infrastructure. It sits alongside your clusters and adds the things that are a pain to build yourself.

## What you get by connecting

**A public front door for your apps.** Claim a [subdomain](./subdomains.md) like `mycluster.run.garden` and your apps get a real address. With [Miren Anywhere](./miren-anywhere.md), they stay reachable from the internet even when your cluster has no public IP, so a home lab or a box behind NAT can serve real traffic.

**Your team.** Bring other people in and control who can do what with role-based access, instead of sharing one set of credentials.

**Multiple environments.** Run separate clusters for production, staging, and preview work under one account, and switch between them from the CLI.

## You still own everything

Connecting to Miren Cloud is additive, not lock-in. Miren runs fully standalone, and if you'd rather keep it that way you can install with cloud turned off:

```bash
sudo miren server install --without-cloud
```

A standalone cluster reaches the network however you've wired it, and none of the cloud features above apply. You can always connect later.

## Getting connected

When you run `miren server install`, Miren registers the cluster with Miren Cloud and walks you through creating your account. You sign in with GitHub or Google, and you can connect both to the same account (see [Accounts and sign-in](./accounts.md)). To connect a server you installed standalone, register it after the fact:

```bash
miren server register -n my-cluster
```

Then log in from whatever machine you work on:

```bash
miren login
```

The [Getting Started](../getting-started.md) guide covers the full install-and-first-deploy flow. For everything the CLI can do with clusters, see the [`miren cluster`](../command/cluster.md) reference.

## Using Cloud from the CLI

The `miren cloud` commands call Cloud's HTTP API using your logged-in identity.
They do not require adding a cluster to your local configuration. If you have
multiple identities, select one with `--identity <name>`; its issuer determines
which Cloud instance receives the request.

```bash
miren cloud org list
miren cloud cluster list
miren cloud app list --organization <org-id>
miren cloud cluster apps --organization <org-id> --cluster <cluster-id>
miren cloud app show --organization <org-id> --cluster <cluster-id> --app myapp
```

`--organization` (or `-o`) accepts an `org-` ID or an exact organization name from
`cloud org list`. Values without the `org-` prefix are looked up by name using
your selected Cloud identity. If you belong to exactly one organization, you can
omit the flag; otherwise specify it explicitly. Duplicate names require an ID.
Cluster flags still take Cloud cluster IDs, not local configuration names.
App detail includes deployment metadata and any available incidents.

```bash
miren cloud app list -o "My Team"
miren cloud app list # Uses your only organization, if you have exactly one
miren cloud me
```

`cloud me` shows a readable profile, organization memberships and enabled
features, and linked sign-in identities by default. Missing membership or
identity data is marked as not reported, since Cloud can return a partial
profile. Use `cloud me --json` for the complete API response.

:::info[Mirrored app state]
App visibility is Cloud's mirrored state, not a live runtime query. Check
observation timestamps, and treat missing health samples as unknown.
:::

For a specific app, the following commands accept the same `--organization`,
`--cluster`, and `--app` flags:

| Command | Result |
| --- | --- |
| `miren cloud app deployments` | Latest 50 deployments, with a truncation indication |
| `miren cloud app deployment --deployment <id>` | Full details of one deployment |
| `miren cloud app routes` | Routes and their sync state |
| `miren cloud app logs` | A bounded snapshot of live app logs |

Logs support `--kind app` or `--kind build`, `--version v3`, `--service <name>`,
`--since 30m`, and `--limit 500` (Cloud caps this at 2000). Live logs also require
runtime authorization and a reachable cluster; they do not follow a stream.

To approve a pending server registration without opening a browser, copy the
registration ID and signed token from the approval URL printed by
`miren server register`, then run:

```bash
miren cloud cluster approve --registration <registration-id> \
  --registration-token <token> --organization <org-id>
```

This creates the cluster in that organization. Use `--name <name>` to override
its requested name. Treat the signed token as a credential.

Every Cloud command supports `--format json` (or `--json`). JSON output preserves
the complete API response, including list envelopes, null values, observation
timestamps, and sync/truncation metadata; progress goes to stderr. List commands
default to tables. Detailed app, deployment, and route responses default to
formatted JSON so nested information is not lost.

### Organization and infrastructure APIs

The [Cloud command reference](../command/cloud.md) includes the following
additional API families. Every organization-scoped command takes an
`--organization <name-or-id>` / `-o` flag, defaulting to your only organization;
group detail and membership commands take `--group`
instead, because group IDs are globally addressable.

| Command family | Capabilities |
| --- | --- |
| `cloud me` | Profile, organization membership, and enabled Cloud features |
| `cloud account` | Update your profile; list/unlink sign-in identities; list/revoke keys; revoke all tokens |
| `cloud user` | Find existing Cloud users to add to an organization |
| `cloud org` | Create/show organizations, search users, list organization clusters |
| `cloud org member` | List, add, remove, and set member roles |
| `cloud group` | Create, show, list, update, and delete access groups; manage their members |
| `cloud cluster` | Delete records, metadata, nodes, routes, connectivity, server status, operation history, upgrade policy |
| `cloud invite` | Manage shared links; inspect and accept invitations as a recipient |
| `cloud enroll-token` | Create, list, inspect, and revoke single-use cluster enrollment credentials |
| `cloud subdomain` | List base domains, claim hostnames, assign/unassign clusters, release claims |
| `cloud volume` | Paginated volume/lease inventory and snapshot/update artifacts |

Admin-only operations are marked in command help. Cloud enforces membership,
admin roles, and feature availability; a disabled or unconfigured API can return
404. Server commands inspect status and history; they do not trigger restarts or
upgrades. `cloud cluster online` requires `--cluster` but not `--organization`, and
Cloud checks the caller's cluster access rules as well as membership.

```bash
miren cloud org member list --organization <org-id>
miren cloud cluster show --organization <org-id> --cluster <cluster-id>
miren cloud cluster update --organization <org-id> --cluster <cluster-id> \
  --tags '{"environment":"production"}' --anywhere-mode auto
miren cloud enroll-token create --organization <org-id> --ttl-hours 24 \
  --default-tags '{"environment":"staging"}' --json
miren cloud subdomain claim --organization <org-id> --name myteam
miren cloud volume list --organization <org-id> --limit 50 --json
```

Structured inputs such as `--tags`, `--default-tags`, and invite `--groups` are
JSON values, not strings containing JSON on the wire. Omitted invite update flags
leave their values unchanged; `--max-uses null` removes the use cap, and
`--groups '[]'` clears automatic group assignments. Cluster and group
`--description` takes plain text: `--description "New description"` sets it and
`--clear-description` clears it. Omitted description flags preserve the current
value; specifying a description and its clear flag together is an error.

Volume listings return `next_cursor` in JSON and print it below text tables.
Pass that value as `--cursor` to fetch the next page. Token listings instead
support `--limit` and `--offset`. Successful no-content API responses render as
JSON `null`.

:::warning[Credentials and destructive operations]
Enrollment creation returns the secret token and bootstrap configuration;
invitation creation/copy returns a credential-bearing link. Keep that output
private. Enrollment creation can reuse an existing live token when no custom
settings are supplied. Revokes, group deletion, member removal, and subdomain
unassignment/release, identity unlinking, and cluster deletion require interactive
confirmation or `--force` in scripts.
:::

### Invitations, accounts, and cluster records

Recipient actions use the credential token from an invitation URL, not an invite
link ID, and do not require `--organization` or organization-admin access:

```bash
miren cloud invite show --token <invitation-token>
miren cloud invite accept --token <invitation-token>
```

Acceptance is successful if you already belong to the organization. To decline,
simply leave the invitation unaccepted; the shared link remains live.

To add an existing account directly, first discover its user XID:

```bash
miren cloud user search --query you@example.com
miren cloud org member add --organization <org-id> --user <user-xid>
```

Global user search finds candidates across Cloud; `cloud org user-search`
searches only existing organization members for group membership management.
For someone without a Cloud account, create an invitation link instead.

```bash
miren cloud account update --name "Your Name"
miren cloud account update --clear-avatar
miren cloud account identity list
miren cloud account identity unlink --provider github --provider-id <github-id>
miren cloud account key list
miren cloud account key revoke --key <key-xid>
miren cloud account token revoke-all --reason "Lost device"
```

Profile and group updates preserve omitted fields. Use `--clear-avatar` to remove
an avatar, or `cloud group update --group <id> --clear-description` to remove a
group description. At least one update field is required; empty string values
are treated as omitted, so use the explicit clear flags.

:::warning[Replacement API concurrency]
Profile and group updates fetch current values before sending a replacement PUT
when fields are omitted. Cloud has no conditional-write support: another edit
between that read and write can be overwritten. Avoid concurrent updates to the
same profile or group.
:::

Identity unlinking takes the provider account ID shown in its listing,
and Cloud refuses to unlink your last sign-in identity. Key revocation takes the
key's XID, not its numeric database ID; the listing includes revoked keys.

`revoke-all` invalidates all previously issued tokens for your account, including
refresh tokens. It does not delete local credentials. Revocation may require
signing in again; persistent keys are revoked separately. Use `miren logout` to
remove a local login and revoke its refresh token.

```bash
miren server register -n my-cluster
miren cloud cluster online-batch --clusters '["<cluster-id>","<other-cluster-id>"]'
miren cloud cluster delete --organization <org-id> --cluster <cluster-id>
```

Use `miren server register` or enrollment tokens to register a server rather than
creating a disconnected Cloud record manually. Batch connectivity reports only
accessible clusters with successful status lookups. Missing entries are not
evidence that a cluster is offline.

Cluster deletion requires organization-admin access and removes the Cloud
record without clearing the server's local registration files. To unregister a
server and clean up its local state, use `miren server unregister` instead.

## Disconnecting

Connecting is reversible. [`miren server unregister`](../command/server-unregister.md) is the inverse of registering: it removes the cluster from your organization in Miren Cloud, revokes the credentials it was using, and clears its registration locally.

```bash
sudo miren server unregister
```

Your apps and sandboxes keep running throughout. What goes away is everything the cluster got from being part of an organization: its cloud DNS records, Miren Anywhere routing, and cloud-managed access rules, which leaves certificate authentication as the way in. The cluster carries on as a standalone one, exactly as if you had installed it with `--without-cloud`.

This is also how you move a cluster between organizations. Unregister it, `miren login` against the organization you want, then register again. The cluster comes back as a new entry with a fresh identity rather than carrying its old one across, since a cluster record belongs to the organization it was created in.

:::warning[Apps served through Miren Cloud lose their address]
If your apps are reachable at a [subdomain](./subdomains.md) or through [Miren Anywhere](./miren-anywhere.md), that traffic stops once the cluster is unregistered. The apps are still running, but nothing routes to them until you give the cluster an address of its own.
:::

If the cluster entry in Miren Cloud is already gone, or the cluster can't reach the internet to say goodbye, `sudo miren server unregister --local-only` clears the local registration without contacting the cloud.

## Explore

- [Subdomains](./subdomains.md) — Claim a hostname like `mycluster.run.garden` for your apps
- [Miren Anywhere](./miren-anywhere.md) — Serve public apps from a cluster with no public IP
- [Cluster Connectivity](./connectivity.md) — Tell whether your cluster is online, deployable, and serving apps
- [Upgrades](./upgrades.md) — Upgrade a cluster from Miren Cloud, or keep it current automatically
