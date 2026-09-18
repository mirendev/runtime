---
title: "miren runner operations run"
sidebar_label: "runner operations run"
description: "Execute or resume an operation in the foreground (normally launched by miren upgrade)"
---

# miren runner operations run

Execute or resume an operation in the foreground (normally launched by miren upgrade)

## Usage

```bash
miren runner operations run [flags]
```

## Flags

- `--config` — Runner config, for reaching the runner to verify readiness (default: `/var/lib/miren/runner/config.yaml`)
- `--dir` — Operation directory (default: `/var/lib/miren/runner/lifecycle`)
- `--operation` — Operation id to execute or resume

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren runner operations`](./runner-operations.md)
