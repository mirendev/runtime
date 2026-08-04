---
title: "miren runner uninstall"
sidebar_label: "runner uninstall"
description: "Remove systemd service for miren runner"
---

# miren runner uninstall

Remove systemd service for miren runner

## Usage

```bash
miren runner uninstall [flags]
```

## Flags

- `--data-path` — Path to runner data (default: `/var/lib/miren/runner`)
- `--remove-data` — Remove runner data directory

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Uninstall the runner service:**

```bash
miren runner uninstall
```

**Uninstall and remove all runner data:**

```bash
miren runner uninstall --remove-data
```

## See also

- [`miren runner`](/command/runner)
