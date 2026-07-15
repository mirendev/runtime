---
title: "miren rollback"
sidebar_label: "rollback"
description: "Roll back to a previous version"
---

# miren rollback

Roll back to a previous version

## Usage

```bash
miren rollback [flags]
```

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Rollback the app in the current directory:**

```bash
miren rollback
```

**Rollback a specific app:**

```bash
miren rollback -a myapp
```
