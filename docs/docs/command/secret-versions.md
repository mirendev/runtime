---
title: "miren secret versions"
sidebar_label: "secret versions"
description: "Show a secret's versions"
---

# miren secret versions

Show a secret's versions

## Usage

```bash
miren secret versions <path> [flags]
```

## Arguments

- `path` — Secret path, e.g. payments/stripe-key

## Flags

- `--backend, -b` — Backend instance to read (default: cluster)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show every version of a secret:**

```bash
miren secret versions payments/stripe-key
```

## See also

- [`miren secret`](/command/secret)
