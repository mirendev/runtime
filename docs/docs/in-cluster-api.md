---
title: In-Cluster API Access
sidebar_label: In-Cluster API
description: Let code running in a Miren sandbox call the cluster API as itself, using its workload identity token and a role you choose per app.
keywords: [in-cluster, api, workload identity, role, rbac, sandbox, service account, app-admin, cluster-readonly]
---

# In-Cluster API Access

Code running inside a Miren sandbox can call the cluster's own API — deploy, read logs, open a shell, inspect status — authenticating as *itself*, with no API key to store or rotate. It's the Miren equivalent of a Kubernetes pod using its service-account token to reach the Kubernetes API.

Every sandbox already carries a [workload identity token](/workload-identity). That page is about presenting the token *outward* (to AWS, GCP, your own APIs). This page is about pointing it *inward*: using it to talk to Miren, and choosing what a given app's workloads are allowed to do once they do.

:::info[Two directions, one token]
- **[Workload Identity](/workload-identity)** — present the token to *external* services to prove which workload you are.
- **In-Cluster API Access** (this page) — present the same token to the *Miren* API, scoped by a role.
:::

Typical uses: a CI job running inside a sandbox that redeploys its own app, a sidecar that tails its app's logs, an operator tool that reads status across the cluster, or an app that manages its own configuration.

## Connecting

From inside a sandbox, the `miren` CLI just works — it detects that it's running in-cluster and authenticates with the mounted token:

```bash
miren app status        # your own app
miren logs              # your own app's logs
```

No `miren login`, no config file. This assumes the `miren` binary is in your image — nothing mounts it into the sandbox, so a distroless or slim image needs the CLI installed first. If a config file *is* present (for example you ran `miren login` yourself), that wins — so you can still point the CLI at another cluster from inside a sandbox.

Miren wires this up through environment variables it injects into every sandbox:

| Environment variable | Value | Use |
| --- | --- | --- |
| `MIREN_IN_CLUSTER` | `1` | Marks the process as running inside a sandbox |
| `MIREN_API_ADDRESS` | e.g. `10.x.x.1:8443` | The cluster API address, reachable from the sandbox |
| `MIREN_CA_CERT_PATH` | `/var/run/miren/ca.crt` | The cluster CA, used to verify the API's certificate |
| `MIREN_IDENTITY_TOKEN_PATH` | `/var/run/miren/identity-token` | The token presented on each request |

The connection is verified against the cluster CA — it is *not* insecure. Because sandboxes share a network bridge, skipping verification would let a neighbor impersonate the API, so the CA is mounted and checked.

## Roles

A token authenticates as a **role**, and the role decides what it may call. Confinement is per app: an *app-scoped* role can only act on the app the sandbox belongs to, while a *cluster-scoped* role reaches across the whole cluster.

The default is **`app-readonly`** — a fresh app's workloads can read their own status and logs, and nothing else. You opt up from there.

| Role | Scope | What it grants |
| --- | --- | --- |
| `none` | — | Authenticates but authorizes nothing |
| `app-readonly` *(default)* | own app | Read own app status, logs, and deployment history |
| `app-deployer` | own app | `app-readonly` + build and (re)deploy its own app |
| `app-debugger` | own app | `app-readonly` + open a shell / run commands in its own sandboxes |
| `app-admin` | own app | `app-deployer` + edit config/env and exec — everything for its own app except deleting it |
| `cluster-readonly` | cluster | Read status, logs, and infrastructure state across all apps |
| `cluster-deployer` | cluster | `cluster-readonly` + build, deploy, and configure any app (but not create or destroy apps) |
| `cluster-debugger` | cluster | `cluster-readonly` + exec and inspect any app |
| `cluster-admin` | cluster | Broad control across the cluster (see the limits below) |

No role — not even `cluster-admin` — can mint identity tokens for other sandboxes, perform raw entity-store writes, or reconfigure the cluster's network fabric. Those stay on the internal operator (certificate) plane. A role is a bearer token mounted in a sandbox; these limits bound the blast radius if one leaks.

### Choosing a role

There are two ways to set an app's role, and they draw the line between what an app owner can do and what an operator can do.

**In `.miren/app.toml`** — self-service, for app owners:

```toml
name = "my-app"
workload_role = "app-deployer"
```

:::warning[app.toml can only set app-scoped roles]
An app confining a token to its own app isn't an escalation — you already control the app. Declaring a cluster-scoped role here fails the deploy, with a message pointing you at the operator command.
:::

**With the CLI** — the operator path:

```bash
miren app set-workload-role -a my-app app-admin
miren app set-workload-role -a tooling cluster-readonly
```

:::warning[Cluster-scoped roles are operator-only]
`set-workload-role` is not reachable by in-sandbox workloads or app-scoped deploy identities — only a cluster operator can call it. That's what stops an app owner from self-granting cluster-wide access.
:::

You can see an app's current role in `miren app status`.

## Sharp edges

:::warning[A role change takes effect on the next sandbox build]
The role is baked into a sandbox's token when the sandbox is created, and the background token refresh preserves it. Changing a role — via `app.toml` on a redeploy, or `set-workload-role` — applies to sandboxes built *after* the change. A redeploy that reuses an existing pool keeps the old role until those sandboxes are actually rebuilt (for example by a deploy that changes the app, or a restart that recreates instances).
:::

:::warning[Grant cluster roles sparingly]
A cluster-scoped role is a powerful credential sitting on disk inside a sandbox. Prefer the narrowest role that does the job — an `app-*` role for anything that only touches one app — and reserve cluster roles for genuine cross-app tooling. Tokens are short-lived (refreshed on a fixed loop), which bounds exposure, but that is not a substitute for least privilege.
:::

:::warning[Removing `workload_role` from app.toml does not revoke it]
Deleting the line leaves the last value in place — a deploy with no `workload_role` doesn't clear it. To drop back to the minimum, set `workload_role = "none"` explicitly (or `app-readonly`), which revokes on the next build.
:::

:::note[The API is only reachable from inside the cluster]
`MIREN_API_ADDRESS` is the API as seen from the sandbox network. It is not a public endpoint and not a stable value — always read it from the environment rather than hardcoding it.
:::

## See also

- [Workload Identity](/workload-identity) — the token itself, and presenting it to external services
- [app.toml Reference](/app-toml) — the `workload_role` field
