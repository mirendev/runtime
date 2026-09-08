---
title: "miren app delete"
sidebar_label: "app delete"
description: "Delete an application and all its resources"
---

# miren app delete

Delete an application and all its resources

## Usage

```bash
miren app delete <appname> [flags]
```

## Arguments

- `appname` — Name of the app to delete

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Force delete without confirmation

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Delete an app (with confirmation prompt):**

```bash
miren app delete myapp
```

**Delete without confirmation:**

```bash
miren app delete myapp --force
```

## See also

- [`miren app`](./app.md)
