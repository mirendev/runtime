---
title: "miren secret keyring"
sidebar_label: "secret keyring"
description: "Show the cluster keyring and any rotation in flight"
---

# miren secret keyring

Show the cluster keyring and any rotation in flight

Shows the keys that encrypt this cluster's stored secrets: which one new writes use, how old each is, and how many stored versions are still wrapped by each.

A key other than the current one with versions still on it means a rotation is part-way through. That is normal and safe — every key in the ring can still decrypt what it wrapped, so nothing is unreadable while the backfill runs. The old key is retired only once its count reaches zero.

## Usage

```bash
miren secret keyring [flags]
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

**Show the cluster keys:**

```bash
miren secret keyring
```

## See also

- [`miren secret`](/command/secret)
