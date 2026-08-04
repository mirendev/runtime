---
title: "miren runner drain"
sidebar_label: "runner drain"
description: "Cordon a runner and evict its sandboxes onto other nodes"
---

# miren runner drain

Cordon a runner and evict its sandboxes onto other nodes

## Usage

```bash
miren runner drain <node> [flags]
```

## Arguments

- `node` — Runner to drain (name, ID, or short ID)

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--reason` — Optional reason for draining the runner
- `--timeout` — Max seconds to wait for the node to empty (0 uses the server default) (default: `0`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Drain a runner before maintenance:**

```bash
miren runner drain my-runner
```

**Drain with a timeout:**

```bash
miren runner drain my-runner --timeout 300
```

## See also

- [`miren runner`](/command/runner)
