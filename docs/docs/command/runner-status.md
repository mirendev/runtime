---
title: "miren runner status"
sidebar_label: "runner status"
description: "Show runner health and configuration"
---

# miren runner status

Show runner health and configuration

## Usage

```bash
miren runner status [flags]
```

## Flags

- `--config` — Path to runner config (default: `/var/lib/miren/runner/config.yaml`)
- `--data-path` — Path to runner data (default: `/var/lib/miren/runner`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Check runner status:**

```bash
miren runner status
```

## See also

- [`miren runner`](/command/runner)
