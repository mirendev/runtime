---
title: "miren cloud cluster approve"
sidebar_label: "cloud cluster approve"
description: "Approve a pending cluster registration in Miren Cloud"
---

# miren cloud cluster approve

Approve a pending cluster registration in Miren Cloud

## Usage

```bash
miren cloud cluster approve [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--name` — Optional cluster name override
- `--organization` — Organization XID to register the cluster in
- `--registration` — Registration ID from the server approval URL
- `--registration-token` — Signed token from the server approval URL

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud cluster`](./cloud-cluster.md)
