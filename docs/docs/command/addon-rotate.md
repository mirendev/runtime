---
title: "miren addon rotate"
sidebar_label: "addon rotate"
description: "Rotate an addon's backing credential"
---

# miren addon rotate

Rotate an addon's backing credential

Rotating an addon credential generates a new secret, applies it to the running backing engine, and updates the value Miren stores. Consuming apps that embed the credential are redeployed automatically to pick up the new connection details — you do not need to run `miren deploy` or `miren app restart`. Rotation runs asynchronously: the command returns a request id and the rotation controller carries it out.

## Usage

```bash
miren addon rotate <name> [flags]
```

## Arguments

- `name` — Addon name (e.g., miren-valkey)

## Flags

- `--credential` — Which credential to rotate (provider-specific; defaults to the addon's primary credential)
- `--force, -f` — Skip confirmation prompt

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Rotate a Valkey addon's password:**

```bash
miren addon rotate miren-valkey
```

**Rotate without confirmation:**

```bash
miren addon rotate miren-valkey --force
```

## See also

- [`miren addon`](/command/addon)
