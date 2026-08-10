---
title: "miren secret destroy"
sidebar_label: "secret destroy"
description: "Permanently delete a version's value"
---

# miren secret destroy

Permanently delete a version's value

Permanently deletes a version's value. Unlike `miren secret disable`, this cannot be undone: the encrypted payload is dropped, so anything still referencing the version can never resolve again.

Prefer `miren secret disable` when revoking a leaked credential — it fails closed just as hard, and leaves you able to recover if something still needed the value.

## Usage

```bash
miren secret destroy <ref> [flags]
```

## Arguments

- `ref` — Reference naming a version, e.g. payments/stripe-key@x1A

## Flags

- `--backend, -b` — Backend instance holding the secret (default: cluster)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Skip the confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Destroy a version's value for good:**

```bash
miren secret destroy payments/stripe-key@x1A
```

## See also

- [`miren secret`](/command/secret)
