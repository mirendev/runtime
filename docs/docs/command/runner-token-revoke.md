---
title: "miren runner token revoke"
sidebar_label: "runner token revoke"
description: "Revoke a join token"
---

# miren runner token revoke

Revoke a join token

## Usage

```bash
miren runner token revoke <tokenid> [flags]
```

## Arguments

- `tokenid` — ID of the token to revoke

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Revoke a token:**

```bash
miren runner token revoke inv_abc123
```

## See also

- [`miren runner token`](/command/runner-token)
