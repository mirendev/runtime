---
title: "miren app runs"
sidebar_label: "app runs"
description: "List recent task runs"
---

# miren app runs

List recent task runs

## Usage

```bash
miren app runs [flags]
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
miren app runs
```

**Show runs of one task as JSON:**

```bash
miren app runs --task migrate --format json
```

## Subcommands

- [`miren app runs cancel`](/command/app-runs-cancel) — End a run early

## See also

- [`miren app`](/command/app)
