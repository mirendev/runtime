---
title: "miren auth revoke"
sidebar_label: "auth revoke"
description: "Revoke a client certificate so the cluster refuses it"
---

# miren auth revoke

Revoke a client certificate so the cluster refuses it

## Usage

```bash
miren auth revoke <serial> [flags]
```

## Arguments

- `serial` — Certificate serial, in hex or decimal

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--reason, -r` — Why the certificate is being revoked
- `--yes, -y` — Skip confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Revoke a leaked certificate:**

```bash
miren auth revoke 4f1a9c3e7b2d8a6f0c5e1d3b9a7f2e64 --reason "leaked"
```

## See also

- [`miren auth`](./auth.md)
