---
title: "miren runner cordon"
sidebar_label: "runner cordon"
description: "Mark a runner unschedulable without stopping its sandboxes"
---

# miren runner cordon

Mark a runner unschedulable without stopping its sandboxes

## Usage

```bash
miren runner cordon <node> [flags]
```

## Arguments

- `node` — Runner to cordon (name, ID, or short ID)

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--reason` — Optional reason for cordoning the runner

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Cordon a runner:**

```bash
miren runner cordon my-runner
```

**Cordon with a reason:**

```bash
miren runner cordon my-runner --reason "cert rotation"
```

## See also

- [`miren runner`](/command/runner)
