---
title: "miren run list"
sidebar_label: "run list"
description: "List recent task runs"
---

# miren run list

List recent task runs

## Usage

```bash
miren run list [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--limit, -n` — Maximum runs to show (default: `0`)
- `--task` — Only show runs of this task

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

**Show recent runs:**

```bash
miren run list
```

**Show runs of one task as JSON:**

```bash
miren run list --task migrate --format json
```

## See also

- [`miren run`](./run.md)
