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

- `--channel` — Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)
- `--check, -c` — Check for available updates only
- `--force, -f` — Force upgrade even if already up to date
- `--health-timeout` — Health check timeout in seconds (default: `60`)
- `--no-auto-rollback` — Disable automatic rollback on failure
- `--skip-health` — Skip health check after upgrade
- `--version, -V` — Specific version to upgrade to (e.g., v0.2.0)

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

- [`miren runner upgrade rollback`](/command/runner-upgrade-rollback) — Rollback runner to previous version

## See also

- [`miren runner`](/command/runner)
