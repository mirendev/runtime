---
title: "miren secret list"
sidebar_label: "secret list"
description: "List stored secrets"
---

# miren secret list

List stored secrets

## Usage

```bash
miren secret list [flags]
```

## Flags

- `--backend, -b` — Backend instance to list (default: cluster)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List all secrets:**

```bash
miren secret list
```

**List as JSON:**

```bash
miren secret list --format json
```

## See also

- [`miren secret`](/command/secret)
