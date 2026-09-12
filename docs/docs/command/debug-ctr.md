---
title: "miren debug ctr"
sidebar_label: "debug ctr"
description: "Run ctr with miren defaults"
---

# miren debug ctr

Run ctr with miren defaults

## Usage

```bash
miren debug ctr [args...] [flags]
```

## Flags

- `--namespace, -n` — containerd namespace (default: `miren`)
- `--socket` — path to containerd socket

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Subcommands

- [`miren debug ctr nuke`](./debug-ctr-nuke.md) — Nuke a containerd namespace

## See also

- [`miren debug`](./debug.md)
