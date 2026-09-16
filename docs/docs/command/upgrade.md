---
title: "miren upgrade"
sidebar_label: "upgrade"
description: "Upgrade miren (the server or runner and the CLI on a systemd host, otherwise the CLI)"
---

# miren upgrade

Upgrade miren (the server or runner and the CLI on a systemd host, otherwise the CLI)

## Usage

```bash
miren upgrade [flags]
```

## Flags

- `--channel` — Channel to use: 'latest' (stable releases, the default except on a runner host) or 'main' (bleeding edge)
- `--check, -c` — Check for available updates only
- `--force, -f` — Upgrade even if already up to date; without root, upgrade only the CLI even though a server is running
- `--user, -u` — Install the CLI to ~/.miren/release/miren instead of the system location
- `--version, -V` — Specific version to upgrade to (e.g., v0.2.0). A runner host defaults to its coordinator's build

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Upgrade the CLI on a client machine:**

```bash
miren upgrade
```

**Upgrade the server or runner and the CLI on the host that runs it:**

```bash
sudo miren upgrade
```

**Check for updates without installing:**

```bash
miren upgrade --check
```

**Upgrade to a specific version:**

```bash
miren upgrade --version v0.2.0
```
