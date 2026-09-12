---
title: "miren server operations run"
sidebar_label: "server operations run"
description: "Execute or resume an operation in the foreground (normally launched by miren upgrade)"
---

# miren server operations run

Execute or resume an operation in the foreground (normally launched by miren upgrade)

## Usage

```bash
miren server operations run [flags]
```

## Flags

- `--dir` — Operation directory (default: `/var/lib/miren/server/lifecycle`)
- `--health-url` — Health endpoint to verify readiness against (default: derived from the server config)
- `--operation` — Operation id to execute or resume

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren server operations`](./server-operations.md)
