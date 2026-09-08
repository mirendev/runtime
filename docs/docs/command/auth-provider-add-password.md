---
title: "miren auth provider add password"
sidebar_label: "auth provider add password"
description: "Add a shared-password identity provider"
---

# miren auth provider add password

Add a shared-password identity provider

## Usage

```bash
miren auth provider add password <name> [flags]
```

## Arguments

- `name` — Name for this password provider

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--password` — Password (omit to prompt interactively, use @file to read from file)
- `--update` — Overwrite an existing provider with the same name (rotates password)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Add a password provider:**

```bash
miren auth provider add password my-pw --password hunter2
```

## See also

- [`miren auth provider add`](./auth-provider-add.md)
