---
title: "miren version"
sidebar_label: "version"
description: "Print the version"
---

# miren version

Print the version

## Usage

```bash
miren version [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--deps` — Show dependencies
- `--format` — Output format (text, json) (default: `text`)
- `--server, -s` — Also report the version of the active cluster's server

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Print version:**

```bash
miren version
```

**JSON output:**

```bash
miren version --format json
```

**Compare with the cluster's server:**

```bash
miren version --server
```
