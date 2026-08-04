---
title: "miren runner install"
sidebar_label: "runner install"
description: "Install systemd service for miren runner"
---

# miren runner install

Install systemd service for miren runner

## Usage

```bash
miren runner install [flags]
```

## Flags

- `--branch, -b` — Branch to download
- `--config` — Path to runner config (default: `/var/lib/miren/runner/config.yaml`)
- `--coordinator, -c` — Override coordinator address from the token
- `--data-path` — Path to store runner data (default: `/var/lib/miren/runner`)
- `--force, -f` — Overwrite existing service file
- `--labels` — Runner labels (key=value)
- `--listen, -l` — Address this runner will listen on
- `--name` — Human-readable name for this runner (defaults to hostname)
- `--no-start` — Do not start the service after installation
- `--skip-system-check` — Skip minimum system requirements check
- `--token, -t` — Enrollment token from 'miren runner token create'

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Install interactively:**

```bash
miren runner install
```

**Install with token (for automation):**

```bash
miren runner install --token mren_...
```

## See also

- [`miren runner`](/command/runner)
