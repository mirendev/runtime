---
title: "miren app runs cancel"
sidebar_label: "app runs cancel"
description: "End a run early"
---

# miren app runs cancel

End a run early

## Usage

```bash
miren app runs cancel <run> [flags]
```

## Arguments

- `run` — Run to cancel

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

**Cancel a run:**

```bash
miren app runs cancel run/myapp-reindex-4kq2np
```

## See also

- [`miren app runs`](/command/app-runs)
