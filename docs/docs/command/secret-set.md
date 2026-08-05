---
title: "miren secret set"
sidebar_label: "secret set"
description: "Store a secret value"
---

# miren secret set

Store a secret value

Stores a value in the cluster's secret store. The value is encrypted at rest and is never echoed, logged, or written to disk by this command — it travels to the server, which holds the only key.

Each write mints a new immutable version and prints its handle, e.g. `payments/stripe-key@x1A`. Storing a value identical to the current one is reported as unchanged rather than minting a duplicate, so re-running the command is safe.

Rotating a secret does not disturb anything already running. Old versions stay resolvable so that a rollback comes back on the value it originally shipped with.

## Usage

```bash
miren secret set <path> [flags]
```

## Arguments

- `path` — Secret path, e.g. payments/stripe-key

## Flags

- `--backend, -b` — Backend instance to store into (default: cluster)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--value` — Secret value; use @file to read from a file. Prompts with masking when omitted

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

## See also

- [`miren secret`](/command/secret)
