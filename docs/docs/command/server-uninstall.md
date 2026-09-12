---
title: "miren server uninstall"
sidebar_label: "server uninstall"
description: "Remove systemd service for miren server"
---

# miren server uninstall

Remove systemd service for miren server

## Usage

```bash
miren server uninstall [flags]
```

## Flags

- `--backup-dir` — Directory to save backup tarball (default: `.`)
- `--remove-data` — Remove /var/lib/miren directory after backing it up
- `--skip-backup` — Skip backup when removing data (dangerous)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Uninstall the server:**

```bash
miren server uninstall
```

**Uninstall and remove all data:**

```bash
miren server uninstall --remove-data
```

## See also

- [`miren server`](./server.md)
