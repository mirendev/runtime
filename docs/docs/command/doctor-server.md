---
title: "miren doctor server"
sidebar_label: "doctor server"
description: "Check server health and connectivity"
---

# miren doctor server

Check server health and connectivity

## Usage

```bash
miren doctor server [flags]
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

**Check server connectivity:**

```bash
miren doctor server
```

## See also

- [`miren doctor`](/command/doctor)
