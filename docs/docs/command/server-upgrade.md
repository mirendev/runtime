---
title: "miren server upgrade"
sidebar_label: "server upgrade"
description: "Upgrade miren server (deprecated: use 'sudo miren upgrade')"
---

# miren server upgrade

Upgrade miren server (deprecated: use 'sudo miren upgrade')

## Usage

```bash
miren server upgrade [flags]
```

## Flags

- `--channel` — Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)
- `--check, -c` — Check for available updates only
- `--force, -f` — Force upgrade even if already up to date
- `--health-timeout` — Seconds to wait for the restarted server to report ready (default: `0`)
- `--no-auto-rollback` — Disable automatic rollback on failure
- `--release, -r` — Upgrade full release package (not just base)
- `--skip-health` — Deprecated: readiness is always verified
- `--version, -V` — Specific version to upgrade to (e.g., v0.2.0)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Upgrade to the latest version:**

```bash
sudo miren server upgrade
```

**Check for available updates:**

```bash
sudo miren server upgrade --check
```

**Upgrade to a specific version:**

```bash
sudo miren server upgrade --version v0.2.0
```

## Subcommands

- [`miren server upgrade rollback`](./server-upgrade-rollback.md) — Rollback server to previous version

## See also

- [`miren server`](./server.md)
