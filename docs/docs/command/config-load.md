---
title: "miren config load"
sidebar_label: "config load"
description: "Load config and merge it with your current config"
---

# miren config load

Load config and merge it with your current config

## Usage

```bash
miren config load [flags]
```

## Flags

- `--config` — Path to the config file to update
- `--force, -f` — Force the update
- `--input, -i` — Path to the input config file to add
- `--set-active, -a` — Set the active cluster

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Load a config file:**

```bash
miren config load --input cluster-config.yaml
```

**Load and set as active cluster:**

```bash
miren config load --input cluster-config.yaml --set-active
```

## See also

- [`miren config`](./config.md)
