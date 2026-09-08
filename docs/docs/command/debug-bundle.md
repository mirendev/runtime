---
title: "miren debug bundle"
sidebar_label: "debug bundle"
description: "Create a support bundle with system debug information"
---

# miren debug bundle

Create a support bundle with system debug information

## Usage

```bash
miren debug bundle [flags]
```

## Flags

- `--docker-container, -d` — Docker container name to get logs from (default: `miren`)
- `--namespace` — containerd namespace (default: `miren`)
- `--output, -o` — Output file path (default: `miren-debug.tar.gz`)
- `--since, -s` — Include logs since this time (default: `1 day ago`)
- `--socket` — path to containerd socket

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug`](./debug.md)
