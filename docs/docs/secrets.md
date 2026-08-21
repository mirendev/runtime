---
title: Secrets
description: Store credentials encrypted in your cluster and reference them from app config, so the value is never stored in the clear and every deploy records exactly which version it used.
keywords: [secrets, credentials, encryption, rotation, env vars, secret backend]
---

import CliCommand from '@site/src/components/CliCommand';

# Secrets

For a real credential, `miren env set -s` is not enough: it masks the value in output, but the value itself still sits in your config. A secret is the alternative — a credential Miren holds **encrypted**, that your app config **points at** rather than contains.

| | `miren env set -s KEY` | `miren secret set` |
|---|---|---|
| Stored | plaintext in the cluster store | encrypted at rest |
| In CLI output | masked | never present — only the reference is |
| Rotation | edit the variable on each app | rotate once, apps re-pin |
| History | none | immutable versions you can roll back to |

Use `-s` for values that are merely noisy in a terminal. Use a secret for anything that would matter if it leaked.

Miren decrypts a secret only into memory, never back to the cluster store in the clear. This happens at two points by default: when a deploy resolves your reference to a concrete version, and when the container that needs the value is created. A Dockerfile build can add a third — the value is decrypted into the builder's memory for a single build step (see [Using a secret at build time](#using-a-secret-at-build-time)), which BuildKit keeps out of the image unless your own build command copies or prints it.

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
miren env set -e STRIPE_API_KEY --ref payments/stripe-key
```

</CliCommand>

`--backend` defaults to `cluster`, so it is only needed when you have another backend registered.

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

Note the asymmetry: `backend` is optional on the command line but **required** in `app.toml`, where a `ref` without one is rejected rather than assumed. A reference cannot be combined with a value — set one or the other.

## Using a secret at build time

An `[[env]]` reference reaches your app at runtime, not during the build. A build sometimes needs a credential of its own — a private package-registry token for `npm ci` or `bundle install`, a license key needed to compile, a key to pull a private dependency. For those, declare a build secret.

:::info[Dockerfile builds only]
Build secrets reach Dockerfile builds only. You wire each one into a `RUN --mount=type=secret` step yourself, and Miren's automatic language builds have no such step — so declaring a build secret on an app that builds from an auto-detected stack is a build error, not a silent no-op.
:::

Declare the secrets under `[build]` in `app.toml`. Each entry has an `id` — the name your Dockerfile mounts it by — plus a `ref` naming the secret. `backend` names which store to resolve against and defaults to `cluster`, Miren's own built-in store (see [Backends](#backends) for registering others), so you set it only when the secret lives in a different backend. The `id` may contain letters, digits, and `_.-`:

```toml
[build]
dockerfile = "Dockerfile"

[[build.secrets]]
id = "npm_token"
backend = "cluster"
ref = "registry/npm-token"
```

Then consume it in the Dockerfile with a secret mount. The value appears as a file under `/run/secrets/<id>` for the duration of that one `RUN`, and BuildKit places it nowhere else on its own — what your command does with it from there is the subject of the warning below:

```dockerfile
RUN --mount=type=secret,id=npm_token \
    NPM_TOKEN="$(cat /run/secrets/npm_token)" npm ci
```

The credential is resolved to plaintext only in the builder's memory, handed to BuildKit over the build session, and mounted into the single `RUN` that asks for it. It never becomes a build argument, and BuildKit never writes it to the build log or an image layer on its own — so it is not visible in `docker history` or to anyone who later pulls the image. This is the difference from an inline value passed with `-e` or `--sensitive`, which is masked in CLI output but does end up baked into the image.

:::warning[Read the mount, don't print it]
BuildKit keeps the value out of layers and out of its own logs, but it cannot stop your own commands from leaking it. If you copy it into an `ENV`, write it to a file a later layer keeps, or print it to stdout or stderr — an `echo`, a `set -x`, a verbose tool — the value ends up in the image or the build log. Read it inside the `--mount=type=secret` `RUN`, use it there, and don't print it.
:::

A build secret must resolve at build time: if the `ref` cannot be found, or the cluster has no secret backend registered, the build fails rather than proceeding without the credential. Each `id` must be unique within the list and contain only letters, digits, and `_.-`, and the value is capped at 500 KB.

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

On a multi-node cluster the value does cross the network once. A runner holds no key material, so the coordinator decrypts and returns the value over the cluster's authenticated connection, where it lives only long enough to be handed to the container.

Once the container starts, your application holds its own copy for as long as it runs — an environment variable is readable by the process and anything that can inspect it. Miren stops handing the value around; it cannot un-give it to the app you handed it to.

:::warning[Back up the keyring]
The cluster key lives at `<data-path>/server/secrets.keyring`, alongside the cluster's CA key. **If that file is lost, every stored secret is permanently unrecoverable.** Include it in whatever backs up your data path.
:::

## The cluster key

Everything above rotates *secrets*. The key that encrypts them rotates too, and Miren handles it.

Each stored version is sealed with its own data key, and only that data key is encrypted by the cluster key. Rotating the cluster key therefore rewrites a few dozen bytes per version and never touches the values themselves, so the work does not grow with how large your secrets are.

A cluster rotates its key automatically once it passes 90 days old (`secrets.key_rotation_period`; set it to `0` to rotate only on request). To rotate now, which is what an incident calls for:

<CliCommand context="client">

```miren
miren secret rotate-key
miren secret keyring
```

</CliCommand>

```text
KEY          CURRENT  AGE       VERSIONS
Qa1gSJKRrOU  -        3 months  12
p9XmK2vNbQs  ✓        just now  4

Rotating off Qa1gSJKRrOU — 4 versions rewrapped so far.
```

A key other than the current one with versions still on it is a rotation part-way through. Nothing is unreadable while that runs — every key in the ring can still decrypt what it wrapped — and the old key is retired only once its count reaches zero. An interrupted rotation resumes on its own, because the work left is "versions still on the old key", which the cluster can always re-ask.

:::note[Rotating the key does not touch your secrets]
This changes how values are encrypted at rest. It does not change any secret's value or mint new versions, so nothing referencing a secret needs redeploying.
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
