---
title: "miren runner service-status"
sidebar_label: "runner service-status"
description: "Show miren-runner systemd service status"
---

# miren runner service-status

Show miren-runner systemd service status

## Usage

```bash
miren runner service-status [flags]
```

## Flags

- `--follow, -f` — Follow logs in real-time

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show service status:**

```bash
miren runner service-status
```

**Follow service logs:**

```bash
miren runner service-status --follow
```

## See also

- [`miren runner`](/command/runner)
