---
title: "miren secret set"
sidebar_label: "secret set"
description: "Store a secret value"
---

# miren secret set

Store a secret value

Stores a value in the cluster's secret store. The value is encrypted at rest and is never echoed, logged, or written to disk by this command — it travels to the server, which holds the only key.

Each write mints a new immutable version and prints its handle, e.g. `payments/stripe-key@x1A`. Storing a value identical to the current one is reported as unchanged rather than minting a duplicate, so re-running the command is safe.

:::note[Rotation does not touch running apps]
Rotating a secret mints a new version but leaves anything already running on the value it started with. Old versions stay resolvable, so a rollback comes back on the value it originally shipped with.
:::

With `--app`, a new secret belongs to that app, and no other app's config can reference it. The app is assigned only when the secret is created; naming one for an existing secret fails unless the secret already belongs to it.

## Usage

```bash
miren secret set <path> [flags]
```

## Arguments

- `path` — Secret path, e.g. payments/stripe-key

## Flags

- `--app, -a` — App the new secret belongs to; only that app's config can then reference it. Applies when the secret is created, and is never read from app.toml
- `--backend, -b` — Backend instance to store into (default: cluster)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--value` — Secret value, or @file to read one from a file. Prompts with masking when omitted, which is safer — a literal here is recorded in shell history. A trailing newline is trimmed

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Store a secret, prompting with masking:**

```bash
miren secret set payments/stripe-key
```

**Store a secret read from a file:**

```bash
miren secret set tls/cert --value @cert.pem
```

**Store a secret that only one app can use:**

```bash
miren secret set shop/db-password --app shop
```

## See also

- [`miren secret`](./secret.md)
