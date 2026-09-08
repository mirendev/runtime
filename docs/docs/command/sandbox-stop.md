---
title: "miren sandbox stop"
sidebar_label: "sandbox stop"
description: "Stop a sandbox"
---

# miren sandbox stop

Stop a sandbox

## Usage

```bash
miren sandbox stop <sandboxid> [flags]
```

## Arguments

- `sandboxid` — ID of the sandbox to stop

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Stop a sandbox by ID:**

```bash
miren sandbox stop sb_abc123
```

## See also

- [`miren sandbox`](./sandbox.md)
