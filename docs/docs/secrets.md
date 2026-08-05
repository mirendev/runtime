---
title: Secrets
description: Store credentials encrypted in your cluster and reference them from app config, so the value never sits in plaintext and every deploy records exactly which version it used.
keywords: [secrets, credentials, encryption, rotation, env vars, secret backend]
---

import CliCommand from '@site/src/components/CliCommand';

# Secrets

A secret is a credential Miren holds **encrypted**, that your app config **points at** rather than contains. The value is decrypted only in memory, at the moment a container starts.

This is different from marking an env var sensitive:

| | `miren env set -s KEY` | `miren secret set` |
|---|---|---|
| Stored | plaintext in the cluster store | encrypted at rest |
| In CLI output | masked | never present — only the reference is |
| Rotation | edit the variable on each app | rotate once, apps re-pin |
| History | none | immutable versions you can roll back to |

Use `-s` for values that are merely noisy in a terminal. Use a secret for anything that would matter if it leaked.

## Storing a secret

<CliCommand context="client">

```miren
miren secret set payments/stripe-key
```

</CliCommand>

You are prompted with masking; the value is never echoed, logged, or written to disk by the CLI. Each write mints an immutable version and prints its handle:

```text
Stored payments/stripe-key@x1A  (cluster, enabled)
```

That `@x1A` is the version. It is an opaque id, not a counter.

To read a value from a file instead of being prompted — useful for certificates and JSON keys — use `--value @filename`. A trailing newline is trimmed, since a credential with one appended is the most common way a copied token stops working.

<CliCommand context="client">

```miren
miren secret set tls/cert --value @server.pem
```

</CliCommand>

Storing a value identical to the current version is reported as unchanged rather than minting a duplicate, so re-running the command is safe.

## Using a secret in an app

Point an environment variable at it. The value does not travel through your shell, your config file, or your terminal — only the reference does.

<CliCommand context="client">

```miren
miren env set -e STRIPE_API_KEY --backend cluster --ref payments/stripe-key
```

</CliCommand>

Or declare it in `app.toml`, which is safe to commit:

```toml
[[env]]
key = "STRIPE_API_KEY"
backend = "cluster"
ref = "payments/stripe-key"
```

Your app reads `STRIPE_API_KEY` as an ordinary environment variable. Nothing in the app needs to know it came from a secret.

`miren env list` shows the reference rather than a mask, because there is no local value to hide:

```text
NAME             VALUE                              SOURCE
STRIPE_API_KEY   → cluster:payments/stripe-key@x1A  manual
DATABASE_POOL    10                                 manual
```

A reference cannot be combined with a value — set one or the other.

## Pinning: which version a deploy used

Every deploy records the exact secret version it resolved. That pin — the `@x1A` above — is stored in the immutable config for that app version.

This is what makes two things work:

- **"What did we ship?"** is answerable for the most sensitive input a deploy has.
- **Rollback restores the right secret.** Rolling back to an older version brings back the value *that version ran with*, not today's. Miren never deletes a version an old config still points at.

## Rotating

Rotating mints a new version. **Running apps are not disturbed** — there is no surprise mid-run swap.

<CliCommand context="client">

```miren
miren secret set payments/stripe-key
```

</CliCommand>

How the rotation reaches your apps depends on how the reference was authored:

- **Declared in `app.toml`** — the reference floats, so the next `miren deploy` picks up the new version automatically.
- **Set with `miren env set`** — the reference is pinned when you set it, so re-run the same `env set` command to move it. This rolls out immediately.

To hold a variable at a specific version regardless, write the version into the reference: `--ref payments/stripe-key@x1A`. This is how you run a rotation overlap, with one variable tracking the new value and another holding the outgoing one while the change propagates.

## Revoking

Disabling a version stops it resolving. Anything still pointing at it **fails closed** on its next deploy rather than falling back to a different value — a revoked credential must never silently become a working one.

<CliCommand context="client">

```miren
miren secret disable payments/stripe-key@x1A
```

</CliCommand>

Disabling is reversible with `miren secret enable`; the value itself is untouched.

:::warning[Revoking does not reach running processes]
Already-running sandboxes keep the value they started with. Disabling a version prevents new resolutions — it does not reach into processes that already hold the secret. Restart or redeploy the affected apps if you need them off the old value.
:::

:::danger[Destroy cannot be undone]
`miren secret destroy` deletes the value for good, and anything still referencing that version can never resolve again. Prefer `miren secret disable` when responding to a leak: it fails closed just as hard and leaves you able to recover if something still needed the value.
:::

## Seeing what you have

<CliCommand context="client">

```miren
miren secret list
miren secret versions payments/stripe-key
```

</CliCommand>

```text
PATH                 BACKEND  CURRENT  VERSIONS
payments/stripe-key  cluster  @m4Q     @m4Q @x1A(disabled)
registry/npm-token   cluster  @p7R     @p7R
```

Both support `--format json`.

## How it is stored

Each version is sealed with its own random data key, and only that key is encrypted by a cluster key. So:

- Rotating the cluster key re-encrypts a few bytes per version, never the values themselves.
- A leaked data key exposes exactly one version rather than every secret.
- The cluster key only ever performs small fixed-size operations, so it can move behind a KMS later without your stored data changing shape.

Within Miren, the plaintext exists only transiently and only in memory: while a deploy resolves it, and while the container that needs it is being created. It is never written to the cluster store, a sandbox spec, an image layer, or a log.

Once the container starts, your application holds its own copy for as long as it runs — an environment variable is readable by the process and anything that can inspect it. Miren stops handing the value around; it cannot un-give it to the app you handed it to.

:::warning[Back up the keyring]
The cluster key lives at `<data-path>/server/secrets.keyring`, alongside the cluster's CA key. **If that file is lost, every stored secret is permanently unrecoverable.** Include it in whatever backs up your data path.
:::

## Backends

`cluster` is Miren's own store, and is always available. It is one *backend instance* among what a cluster may register — the model is designed so that external managers (Vault, AWS Secrets Manager, GCP Secret Manager) can be registered alongside it under their own names, with references written the same way:

```toml
[[env]]
key = "SIGNING_KEY"
backend = "prod-vault"
ref = "secret/data/storefront#signing"
```

Miren only ever *reads* an external manager — it never writes into one, so that manager stays the source of truth for its own secrets. Only the `cluster` backend ships today.
