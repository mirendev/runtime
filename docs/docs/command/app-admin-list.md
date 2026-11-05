---
title: "miren app admin list"
sidebar_label: "app admin list"
description: "List application-defined admin methods"
---

# miren app admin list

List application-defined admin methods

## Usage

```bash
miren app admin list [method] [args...] [flags]
```

## Arguments

- `method` — Admin method to call

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--func-help` — Show help for a specific admin method
- `--json` — Shorthand for --format json
- `--legacyjson, -j` — Deprecated: use --json or --format json
- `--list, -l` — List available admin methods
- `--no-validate` — Skip method/parameter validation
- `--params-file, -f` — Read params as JSON from file (use - for stdin)
- `--pretty, -p` — Render output in a human-friendly format (default for TTY)

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

## See also

- [`miren app admin`](./app-admin.md)
