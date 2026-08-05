---
title: "miren secret enable"
sidebar_label: "secret enable"
description: "Let a disabled version resolve again"
---

# miren secret enable

Let a disabled version resolve again

## Usage

```bash
miren secret enable <ref> [flags]
```

## Arguments

- `ref` — Reference naming a version, e.g. payments/stripe-key@x1A

## Flags

- `--backend, -b` — Backend instance holding the secret (default: cluster)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Re-enable a version:**

```bash
miren secret enable payments/stripe-key@x1A
```

## See also

- [`miren secret`](/command/secret)
