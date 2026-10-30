---
title: "miren app init"
sidebar_label: "app init"
description: "Initialize a new application"
---

# miren app init

Initialize a new application

## Usage

```bash
miren app init [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--dir, -d` — Application directory (defaults to current directory)
- `--name, -n` — Application name (defaults to directory name)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Initialize in current directory:**

```bash
miren app init
```

**Initialize with a specific name:**

```bash
miren app init --name myapp
```

## See also

- [`miren app`](./app.md)
