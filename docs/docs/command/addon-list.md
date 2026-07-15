---
title: "miren addon list"
sidebar_label: "addon list"
description: "List addons attached to an application"
---

# miren addon list

List addons attached to an application

## Usage

```bash
miren addon list [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

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

**List addons for the current app:**

```bash
miren addon list
```

## See also

- [`miren addon`](/command/addon)
