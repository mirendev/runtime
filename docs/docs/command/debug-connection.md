---
title: "miren debug connection"
sidebar_label: "debug connection"
description: "Test connectivity and authentication with a server"
---

# miren debug connection

Test connectivity and authentication with a server

## Usage

```bash
miren debug connection [flags]
```

## Flags

- `--cluster, -c` — Cluster name from config to test
- `--identity, -i` — Identity name to use for authentication
- `--insecure` — Skip TLS certificate verification
- `--server, -s` — Server hostname or IP address to test directly

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug`](./debug.md)
