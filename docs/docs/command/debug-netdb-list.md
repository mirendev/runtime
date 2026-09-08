---
title: "miren debug netdb list"
sidebar_label: "debug netdb list"
description: "List all IP leases from netdb"
---

# miren debug netdb list

List all IP leases from netdb

## Usage

```bash
miren debug netdb list [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--released, -R` — Show only released IPs
- `--reserved, -r` — Show only reserved (in-use) IPs
- `--subnet, -s` — Filter by subnet CIDR

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug netdb`](./debug-netdb.md)
