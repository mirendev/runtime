---
title: "miren cluster available"
sidebar_label: "cluster available"
description: "List the clusters Miren Cloud has for your account"
---

# miren cluster available

List the clusters Miren Cloud has for your account

## Usage

```bash
miren cluster available [flags]
```

## Flags

- `--check` — Ask cloud whether it can reach the clusters that advertise no address
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Name of the identity to use (optional - will use the only one if single)
- `--json` — Shorthand for --format json
- `--organization` — Only list clusters in this organization

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List clusters you could add:**

```bash
miren cluster available
```

**List as JSON:**

```bash
miren cluster available --format json
```

**Ask cloud whether it can reach the clusters with no address:**

```bash
miren cluster available --check
```

## See also

- [`miren cluster`](/command/cluster)
