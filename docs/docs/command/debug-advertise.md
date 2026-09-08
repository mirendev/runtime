---
title: "miren debug advertise"
sidebar_label: "debug advertise"
description: "Show which addresses the server would advertise and why"
---

# miren debug advertise

Show which addresses the server would advertise and why

## Usage

```bash
miren debug advertise [flags]
```

## Flags

- `--additional-ip` — Simulate a server-configured AdditionalIP (repeatable)
- `--cloud-url` — Cloud URL to use for netcheck (default: https://api.miren.cloud)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--listen` — Simulate the server's listen address (default: 0.0.0.0:8443)
- `--skip-netcheck` — Skip the netcheck call and only report interface scan

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug`](./debug.md)
