---
title: "miren app set-workload-role"
sidebar_label: "app set-workload-role"
description: "Set the API role for an app's sandbox identity tokens"
---

# miren app set-workload-role

Set the API role for an app's sandbox identity tokens

## Usage

```bash
miren app set-workload-role [args...] [flags]
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

**Let an app's workloads read and deploy their own app:**

```bash
miren app set-workload-role -a myapp app-deploy
```

**Grant a cluster-wide read role (operator only):**

```bash
miren app set-workload-role -a tooling cluster-readonly
```

## See also

- [`miren app`](/command/app)
