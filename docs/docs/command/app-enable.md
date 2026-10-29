---
title: "miren app enable"
sidebar_label: "app enable"
description: "Start a disabled app again"
---

# miren app enable

Start a disabled app again

Enable turns a disabled app back on. Fixed-mode services return to their configured count right away; autoscaled services stay at zero and start on the next request.

## Usage

```bash
miren app enable [flags]
```

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Enable an app:**

```bash
miren app enable -a myapp
```

## See also

- [`miren app`](./app.md)
