---
title: Workload Identity
description: Every Miren sandbox receives a signed OIDC identity token it can use to authenticate to external services — no long-lived cloud credentials baked into your app.
keywords: [workload identity, oidc, jwt, aws sts, federation, sandbox, token, identity token]
---

# Workload Identity

Every sandbox running on Miren automatically receives a signed OIDC identity token. Your code can present this token to external services — AWS, GCP, Azure, or your own APIs — to prove *which workload it is* without you storing any long-lived cloud credentials in the app.

It works the same way GitHub Actions' OIDC tokens do: the platform (here, your Miren cluster) acts as an OpenID Connect issuer, signs a short-lived JWT describing the workload, and publishes the public keys so anyone can verify it.

:::info[Workload identity vs. CI/CD OIDC]
These are two sides of the same OIDC machinery, pointed in opposite directions:

- **Workload Identity** (this page) — your cluster issues tokens **for** the sandboxes running on it, so your *running app* can call out to AWS, GCP, etc.
- **[CI/CD Deployment](/ci-deploy)** — your cluster *verifies* tokens issued **by** GitHub/GitLab, so a *pipeline* can deploy to Miren without stored secrets.

Both rely on the cluster's OIDC infrastructure, but the token flows in different directions.
:::

:::tip[Calling the Miren API from a sandbox]
The same token can authenticate to your cluster's own API — deploy, read logs, open a shell — scoped by a role you choose per app. See [In-Cluster API Access](/in-cluster-api).
:::

## Minimum working example

Every sandbox already has a token — no configuration needed. Read it from the mounted file:

```bash
TOKEN=$(cat "$MIREN_IDENTITY_TOKEN_PATH")
```

Or request one scoped to a specific audience from the token server:

```bash
TOKEN=$(curl -s -H "Authorization: Bearer $MIREN_IDENTITY_TOKEN_SECRET" \
  "$MIREN_IDENTITY_TOKEN_URL?audience=sts.amazonaws.com&ttl=900" | jq -r .value)
```

Present the JWT to any service configured to trust your cluster's issuer (`$MIREN_OIDC_ISSUER_URL`) — see [AWS via STS Federation](#aws-via-sts-federation) for the end-to-end flow.

## How It Works

1. **Your cluster is an OIDC issuer.** It owns a signing key and publishes a standard discovery document at `/.well-known/openid-configuration` and its public keys (JWKS) at `/.well-known/miren/jwks`.
2. **Every sandbox gets a token.** When a sandbox starts, Miren mints a JWT describing it (organization, cluster, app, sandbox ID), writes it into the container, and refreshes it on a background loop.
3. **Your app presents the token** to an external service that's been configured to trust your cluster as an identity provider.
4. **The external service verifies it** by fetching your cluster's discovery document and JWKS, checking the signature, and matching the token's claims against its own access rules — then grants short-lived, scoped credentials.

No secret is shared in advance. The external system trusts your cluster's *issuer URL* and verifies signatures against its published keys.

## Two Ways to Get a Token

A sandbox can obtain its identity token two ways:

1. **Read the file** at `/var/run/miren/identity-token` — the simplest path, always present, refreshed for you.
2. **Call the token server** when you need a token with a specific audience or a shorter lifetime than the standard refresh provides.

Both are wired up through environment variables that Miren injects into every sandbox:

| Environment variable | Value | Use |
| --- | --- | --- |
| `MIREN_IDENTITY_TOKEN_PATH` | `/var/run/miren/identity-token` | Path to the auto-refreshed token file |
| `MIREN_OIDC_ISSUER_URL` | e.g. `https://cluster.example.com` | The cluster's issuer; matches the token's `iss` claim |
| `MIREN_IDENTITY_TOKEN_URL` | e.g. `http://10.x.x.1:7123/v1/token` | On-demand token endpoint |
| `MIREN_IDENTITY_TOKEN_SECRET` | a 32-byte hex secret | Bearer credential for the token endpoint |

Prefer these environment variables over hardcoding paths or URLs — the token-server address in particular is internal and not a stable value.

## The Identity Token File

The simplest way to use workload identity is to read the file:

```bash
$ cat "$MIREN_IDENTITY_TOKEN_PATH"
eyJhbGciOiJSUzI1NiIsImtpZCI6...
```

- It's a standard signed JWT, mounted **read-only**.
- Miren refreshes it **in place** on a background loop (roughly every 45 minutes), well before it expires.

Because the file is refreshed in place, **read it fresh each time you need it** rather than caching the contents at startup. If your workload runs for a long time and reads the token only once, you may end up holding a token that's about to expire. When in doubt — or when you need a custom audience or a shorter TTL — use the token server instead.

## The Token Server

For tokens with a specific audience or a custom lifetime, call the on-demand token server. It's a small HTTP endpoint reachable from inside the sandbox at `MIREN_IDENTITY_TOKEN_URL`.

```bash
$ curl -H "Authorization: Bearer $MIREN_IDENTITY_TOKEN_SECRET" \
  "$MIREN_IDENTITY_TOKEN_URL?audience=sts.amazonaws.com&ttl=900"
```

Response:

```json
{ "value": "eyJhbGciOiJSUzI1NiIsImtpZCI6..." }
```

**Request**

- Method: `GET` only (other methods return `405`).
- Auth: `Authorization: Bearer $MIREN_IDENTITY_TOKEN_SECRET`. The secret is unique per sandbox.
- Query parameters (both optional):
  - `audience` — the intended recipient(s) of the token. Repeat the parameter for multiple audiences. Defaults to `miren` if omitted.
  - `ttl` — token lifetime in seconds. Default `3600` (1 hour), minimum `60`, maximum `86400` (24 hours).

**Errors**

| Status | Meaning |
| --- | --- |
| `400` | Bad request (e.g. `ttl` out of range or not a number) |
| `401` | Missing or malformed `Authorization` header |
| `403` | Bearer token doesn't match the requesting sandbox |
| `405` | Method other than `GET` |
| `500` | Token issuance failed |

## What's in a Token

Each token is a JWT carrying the standard registered claims plus a few Miren-specific ones describing the workload:

| Claim | Description |
| --- | --- |
| `iss` | Issuer — your cluster's OIDC URL (same as `MIREN_OIDC_ISSUER_URL`) |
| `sub` | Subject — a structured identity string (see below) |
| `aud` | Audience — who the token is for (defaults to `miren`, or what you requested) |
| `exp`, `iat`, `nbf` | Expiry, issued-at, and not-before timestamps |
| `jti` | Unique token ID |
| `organization_id` | Your organization (for cloud-registered clusters) |
| `cluster_id` | The cluster that issued the token |
| `app` | The application name |
| `sandbox_id` | The sandbox instance |
| `identity_type` | The kind of principal the token represents (always `sandbox` for the tokens your app receives) |

The `sub` (subject) encodes the workload's identity as a path-like string, omitting any empty parts:

```
org:<organization_id>:app:<app>:sandbox:<sandbox_id>
```

:::warning[Subject delimiter]
Each label and value is one colon-delimited segment. Miren refuses to mint a
token when any segment contains `:`, rather than emit a subject that could be
decoded as a different identity.
:::

A decoded token payload looks like:

```json
{
  "iss": "https://cluster-aabbcc.miren.systems",
  "sub": "org:org-demo-xyz:app:demo:sandbox:sandbox/demo-web-xxyyzz",
  "aud": "sts.amazonaws.com",
  "exp": 1718053200,
  "iat": 1718049600,
  "nbf": 1718049600,
  "jti": "a1b2c3d4-...",
  "organization_id": "org-demo-xyz",
  "cluster_id": "cluster-aabbcc",
  "app": "demo",
  "sandbox_id": "sandbox/demo-web-xxyyzz",
  "identity_type": "sandbox"
}
```

:::note[System workload tokens]
Tokens issued to your sandboxes always carry `identity_type: "sandbox"`. Miren
issues tokens to its own system workloads as well, with a different value and a
different subject shape, but those are never handed to an app and aren't
something you request.
:::

External systems use these claims to decide what a token is allowed to do — for example, an AWS role trust policy can require a specific `sub` or `aud` before handing back credentials.

## Use Cases

### AWS via STS Federation

The canonical use case: let a sandbox assume an AWS IAM role and receive temporary credentials, with no `AWS_ACCESS_KEY_ID` stored anywhere.

1. **In AWS**, register your cluster as an OIDC identity provider (its issuer URL is `MIREN_OIDC_ISSUER_URL`) and create an IAM role whose trust policy federates that provider. Scope the trust policy with a condition on the token's `sub` or `aud` so only the workloads you intend can assume the role.

2. **In your sandbox**, request a token scoped to STS and exchange it:

   ```bash
   TOKEN=$(curl -s --get "$MIREN_IDENTITY_TOKEN_URL" \
     -H "Authorization: Bearer $MIREN_IDENTITY_TOKEN_SECRET" \
     --data-urlencode "audience=sts.amazonaws.com" | jq -r .value)

   aws sts assume-role-with-web-identity \
     --role-arn arn:aws:iam::123456789012:role/miren-web \
     --role-session-name web \
     --web-identity-token "$TOKEN"
   ```

   AWS verifies the token against your cluster's published keys, checks the trust policy, and returns short-lived credentials.

### GCP and Azure

Both Google Cloud and Azure support OIDC-based **workload identity federation**. Configure a workload identity pool / federated credential that trusts your cluster's issuer URL and matches on the token's subject or audience, then exchange the Miren token for cloud credentials using each provider's federation flow. The mechanics differ per provider, but the trust relationship is the same: they verify the token against your cluster's JWKS.

For the provider-specific setup, see:

- [GCP Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation) — create a workload identity pool with an OIDC provider pointing at your cluster's issuer URL.
- [Azure workload identity federation](https://learn.microsoft.com/entra/workload-id/workload-identity-federation) — add a federated credential to an app registration or managed identity, using your cluster as the issuer.

### Internal Service-to-Service Auth

Your own services can accept Miren identity tokens directly. A service verifies the token against the cluster's JWKS (see [Verifying Tokens](#verifying-tokens) below) and then authorizes based on the claims — for example, only accepting requests from a particular `app`.

### Per-Workload Access Control

Because each token carries `organization_id`, `cluster_id`, `app`, and `sandbox_id`, downstream systems can make fine-grained decisions: grant one app access to one bucket, gate a multi-tenant API on `organization_id`, or trace a request back to the exact sandbox that made it.

## Verifying Tokens

Any system that wants to trust Miren-issued tokens follows the standard OIDC verification flow:

1. **Discover** — fetch the discovery document at:

   ```
   <issuer>/.well-known/openid-configuration
   ```

   It advertises the issuer, the `jwks_uri`, and supported signing algorithms.

2. **Fetch keys** — retrieve the JSON Web Key Set at:

   ```
   <issuer>/.well-known/miren/jwks
   ```

   Tokens are signed with **RS256** by default, which every standard OIDC verifier supports.

3. **Verify** — check the JWT signature against the JWKS, confirm the token isn't expired, and **pin the issuer**: the token's `iss` claim must exactly match the issuer URL you trust.

The issuer URL is your cluster's public hostname. For clusters registered with Miren Cloud it's the provisioned DNS name; for self-hosted clusters it's the first TLS name configured for the cluster. Either way it's the value exposed to sandboxes as `MIREN_OIDC_ISSUER_URL`.

## Sharp Edges & Limitations

### Federation needs a hostname; identity itself doesn't

Workload identity turns on automatically — there's no per-app setting to enable it — and every cluster issues tokens, including one installed with `--without-cloud` and no TLS name. Your sandboxes always get the `MIREN_IDENTITY_*` variables.

What such a cluster lacks is a hostname an outside party can resolve. It anchors its tokens at `https://cluster.local`, which nothing outside the cluster can fetch a discovery document from, so **external federation won't work**: AWS, GCP, and Azure all need to reach your issuer URL to fetch its keys. Give the cluster a real name (register it with Miren Cloud, or pass `--dns-names`) and its tokens become federatable, with no other change.

:::note[Why tokens exist either way]
Miren's own services authenticate to each other with these tokens — the cluster-local registry and the telemetry that distributed runners ship both verify them against the signing key in-process, which needs no DNS and no publicly trusted certificate. Tying identity to being externally addressable would leave those services with nothing but the network to trust.
:::

### The file refreshes on a fixed loop — read it fresh

The token file is refreshed roughly every 45 minutes, in place. This interval is an internal detail, not a tunable. The practical consequence: **don't cache the file's contents** for the life of a long-running process. Re-read `MIREN_IDENTITY_TOKEN_PATH` each time you need a token, or use the token server when you need control over lifetime or audience.

### Token-server address is internal

`MIREN_IDENTITY_TOKEN_URL` points at an internal router address and a fixed port (7123). Always use the injected environment variable rather than hardcoding either — they are implementation details and may change.

### Distributed runners issue tokens via the coordinator

In a cluster with [distributed runners](/distributed-runners), only the coordinator holds the signing key. On a distributed runner, token issuance is proxied back to the coordinator over RPC. Two consequences worth knowing:

- There's a small amount of extra latency, and issuance depends on the coordinator being reachable.
- A runner that can't reach the coordinator's issuer at startup disables token issuance for its sandboxes, so they won't get the `MIREN_IDENTITY_*` variables. The coordinator always has an issuer, so in practice this means a connectivity problem rather than a configuration one.

### Restarts and the token-server secret

The token server authenticates on-demand requests using a per-sandbox secret (`MIREN_IDENTITY_TOKEN_SECRET`) held in an in-memory registry. To survive a controller or server restart, each secret is also persisted host-side and re-registered for still-running sandboxes during boot reconciliation. This is handled for you; it's documented here so the behavior isn't surprising if you're inspecting the host filesystem.

### Key rotation is operator-driven

The cluster's signing key lives alongside the server data. Rotation supports an overlap window so in-flight tokens keep verifying:

- The current key can be demoted to a `.prev` file, which is still published in the JWKS for verification but no longer used to sign.
- Additional live keys can be placed in a `workload-identity.d/` directory; all live keys are published, but only the primary signs.
- Rotation is **not** automatic — an operator must move keys deliberately, and clear a stale `.prev` before rotating again.
