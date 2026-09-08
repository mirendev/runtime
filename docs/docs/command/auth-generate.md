---
title: "miren auth generate"
sidebar_label: "auth generate"
description: "Generate authentication config file"
---

# miren auth generate

Generate authentication config file

## Usage

```bash
miren auth generate [flags]
```

## Flags

- `--cluster-name, -C` — Name of the cluster (default: `local`)
- `--config-path, -c` — Path to the config file, - for stdout (default: `clientconfig.yaml`)
- `--data-path, -d` — Data path (default: `/var/lib/miren`)
- `--name, -n` — Name of the client certificate (default: `miren-user`)
- `--public-ip, -p` — Use public IP for the target, if available
- `--target, -t` — Hostname to embed in the config (default: `localhost`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Generate auth config:**

```bash
miren auth generate
```

## See also

- [`miren auth`](./auth.md)
