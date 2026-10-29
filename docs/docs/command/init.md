---
title: "miren init"
sidebar_label: "init"
description: "Alias for 'miren app init'"
---

# miren init

Alias for 'miren app init'

## Usage

```bash
miren init [flags]
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
