---
title: "miren runner uncordon"
sidebar_label: "runner uncordon"
description: "Make a cordoned runner eligible for scheduling again"
---

# miren runner uncordon

Make a cordoned runner eligible for scheduling again

## Usage

```bash
miren runner uncordon <node> [flags]
```

## Arguments

- `node` — Runner to uncordon (name, ID, or short ID)

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Uncordon a runner:**

```bash
miren runner uncordon my-runner
```

## See also

- [`miren runner`](/command/runner)
