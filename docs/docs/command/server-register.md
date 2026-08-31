---
title: "miren server register"
sidebar_label: "server register"
description: "Register this cluster with miren.cloud"
---

# miren server register

Register this cluster with miren.cloud

## Usage

```bash
miren server register [flags]
```

## Flags

- `--enroll-token` — Unattended enroll token from miren.cloud
- `--name, -n` — Cluster name
- `--output, -o` — Output directory for registration (default: `/var/lib/miren/server`)
- `--url, -u` — Cloud URL (default: `https://miren.cloud`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Register with cloud:**

```bash
miren server register --name my-cluster
```

**Register with a specific cloud URL:**

```bash
miren server register --name my-cluster --url https://cloud.example.com
```

## Subcommands

- [`miren server register status`](/command/server-register-status) — Show cluster registration status

## See also

- [`miren server`](/command/server)
