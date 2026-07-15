---
title: "miren auth ci list"
sidebar_label: "auth ci list"
description: "List CI authentication bindings for an application"
---

# miren auth ci list

List CI authentication bindings for an application

## Usage

```bash
miren auth ci list [flags]
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

## See also

- [`miren auth ci`](/command/auth-ci)
