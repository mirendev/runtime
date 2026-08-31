---
title: "miren server install"
sidebar_label: "server install"
description: "Install systemd service for miren server"
---

# miren server install

Install systemd service for miren server

## Usage

```bash
miren server install [flags]
```

## Flags

- `--address, -a` — Server address to bind to (default: `0.0.0.0:8443`)
- `--branch, -b` — Branch to download if release not found
- `--enroll-token` — Unattended enroll token from miren.cloud (registers without browser approval)
- `--force, -f` — Overwrite existing service file
- `--name, -n` — Cluster name for cloud registration
- `--no-start` — Do not start the service after installation
- `--skip-system-check` — Skip minimum system requirements check
- `--url, -u` — Cloud URL for registration (default: `https://miren.cloud`)
- `--verbosity` — Extra verbosity to bake into the unit (e.g. -v); the server defaults to Info without it
- `--without-cloud` — Skip cloud registration setup

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Install with cloud registration:**

```bash
miren server install
```

**Install without cloud (local only):**

```bash
miren server install --without-cloud
```

**Install with an unattended enroll token:**

```bash
miren server install --enroll-token "$(cat /etc/miren/enroll-token)"
```

## See also

- [`miren server`](/command/server)
