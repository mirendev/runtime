---
title: "miren doctor config"
sidebar_label: "doctor config"
description: "Check configuration files"
---

# miren doctor config

Check configuration files

## Usage

```bash
miren doctor config [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Check config files:**

```bash
miren doctor config
```

## See also

- [`miren doctor`](./doctor.md)
