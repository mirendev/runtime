---
title: "miren debug saga list"
sidebar_label: "debug saga list"
description: "List saga executions"
---

# miren debug saga list

List saga executions

By default this lists only active sagas, meaning the ones that are pending, running, or undoing. Completed sagas accumulate and are rarely what you are looking for when something is wedged. Pass `--all` to include them, or `--status` to ask for one specific status.

The UPDATED column is the useful one for finding a stuck saga: the record is written after every action, so a saga that has been running for an hour without an update has stopped making progress.

```bash
miren debug saga list
miren debug saga list --status failed
miren debug saga list --definition provision_mysql_dedicated --all
miren debug saga list --format json
```

## Usage

```bash
miren debug saga list [flags]
```

## Flags

- `--all, -A` — Include completed and failed sagas
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--definition, -d` — Filter by saga definition name
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--status, -s` — Filter by status (pending, running, undoing, completed, failed)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug saga`](/command/debug-saga)
