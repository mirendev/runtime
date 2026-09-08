---
title: "miren server upgrade"
sidebar_label: "server upgrade"
description: "Upgrade miren server"
---

# miren server upgrade

Upgrade miren server

## Usage

```bash
miren server upgrade [flags]
```

## Flags

- `--channel` — Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)
- `--check, -c` — Check for available updates only
- `--force, -f` — Force upgrade even if already up to date
- `--health-timeout` — Health check timeout in seconds (default: `60`)
- `--no-auto-rollback` — Disable automatic rollback on failure
- `--release, -r` — Upgrade full release package (not just base)
- `--skip-health` — Skip health check after upgrade
- `--version, -V` — Specific version to upgrade to (e.g., v0.2.0)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Upgrade to the latest version:**

```bash
miren server upgrade
```

**Check for available updates:**

```bash
miren server upgrade --check
```

**Upgrade to a specific version:**

```bash
miren server upgrade --version v0.2.0
```

## Subcommands

- [`miren server upgrade rollback`](./server-upgrade-rollback.md) — Rollback server to previous version

## See also

- [`miren server`](./server.md)
