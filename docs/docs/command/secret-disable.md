---
title: "miren secret disable"
sidebar_label: "secret disable"
description: "Stop a version from resolving"
---

# miren secret disable

Stop a version from resolving

Stops a specific version from resolving. Anything still referencing it fails closed on its next resolve rather than falling back to a different value — a revoked secret must never silently become a working one.

Disabling is reversible with `miren secret enable`; the value itself is untouched. To delete the value outright, use `miren secret destroy`.

## Usage

```bash
miren secret disable <ref> [flags]
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

**Revoke a leaked version:**

```bash
miren secret disable payments/stripe-key@x1A
```

## See also

- [`miren secret`](/command/secret)
