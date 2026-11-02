---
title: "miren runner remove"
sidebar_label: "runner remove"
description: "Deprecated: use 'miren runner delete'"
---

# miren runner remove

Deprecated: use 'miren runner delete'

## Usage

```bash
miren runner remove <node> [flags]
```

## Arguments

- `node` — Runner to remove (name, ID, or short ID)

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Force removal even if the runner has active sandboxes
- `--yes, -y` — Skip confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Remove a runner by name:**

```bash
miren runner delete my-runner
```

**Force remove a runner with active sandboxes:**

```bash
miren runner delete my-runner --force
```

## See also

- [`miren runner`](./runner.md)
