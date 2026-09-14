---
title: "miren cluster upgrade"
sidebar_label: "cluster upgrade"
description: "Upgrade the active cluster's server and wait for it to report ready"
---

# miren cluster upgrade

Upgrade the active cluster's server and wait for it to report ready

## Usage

```bash
miren cluster upgrade [flags]
```

## Flags

- `--channel` — Channel to use: 'latest' (stable releases, default) or 'main' (bleeding edge)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--health-timeout` — Seconds to wait for the restarted server to report ready (default: `0`)
- `--no-auto-rollback` — Disable automatic rollback on failure
- `--release, -r` — Upgrade full release package (not just base)
- `--version, -V` — Specific version to upgrade to (e.g., v0.2.0)
- `--yes, -y` — Skip the confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Upgrade a named cluster to the latest release:**

```bash
miren cluster upgrade -C production
```

**Upgrade to a specific version without prompting:**

```bash
miren cluster upgrade --version v0.16.0 --yes
```

## See also

- [`miren cluster`](./cluster.md)
