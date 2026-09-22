---
title: "miren runner upgrade"
sidebar_label: "runner upgrade"
description: "Upgrade miren runner to the latest or specified version"
---

# miren runner upgrade

Upgrade miren runner to the latest or specified version

## Usage

```bash
miren runner upgrade [flags]
```

## Flags

- `--channel` — Channel to use instead of matching the coordinator: 'latest' (stable releases) or 'main' (bleeding edge)
- `--check, -c` — Check for available updates only
- `--force, -f` — Force upgrade even if already up to date
- `--health-timeout` — Seconds to wait for the restarted runner to report ready (default: `0`)
- `--no-auto-rollback` — Disable automatic rollback on failure
- `--skip-health` — Deprecated: readiness is always verified
- `--version, -V` — Specific version to upgrade to (e.g., v0.2.0); default is the coordinator's build

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Upgrade to the latest version:**

```bash
miren runner upgrade
```

**Check for available updates:**

```bash
miren runner upgrade --check
```

**Upgrade to a specific version:**

```bash
miren runner upgrade --version v0.2.0
```

## Subcommands

- [`miren runner upgrade rollback`](./runner-upgrade-rollback.md) — Rollback runner to previous version

## See also

- [`miren runner`](./runner.md)
