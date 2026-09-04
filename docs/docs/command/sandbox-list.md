---
title: "miren sandbox list"
sidebar_label: "sandbox list"
description: "List sandboxes (excludes dead by default)"
---

# miren sandbox list

List sandboxes (excludes dead by default)

List the sandboxes currently known to the cluster. Dead sandboxes are hidden unless you pass `--all` or ask for the `dead` status explicitly.

The JSON form is a stable inventory view with the same operational fields as the table. It deliberately excludes the raw sandbox execution spec, which contains resolved environment values and may include secrets. For low-level inspection, use the explicitly privileged `miren debug entity list -k sandbox` command.

## Usage

```bash
miren sandbox list [flags]
```

## Flags

- `--all, -a` — Include dead sandboxes (excluded by default)
- `--app` — Only show sandboxes belonging to this app
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--service` — Only show sandboxes of this service (e.g. web, worker)
- `--status, -s` — Filter by status (pending, not_ready, running, stopped, dead)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List running sandboxes:**

```bash
miren sandbox list
```

**Include dead sandboxes:**

```bash
miren sandbox list --all
```

**Show only one app's sandboxes:**

```bash
miren sandbox list --app myapp
```

**Show only one service of an app:**

```bash
miren sandbox list --app myapp --service worker
```

**List as JSON:**

```bash
miren sandbox list --format json
```

## See also

- [`miren sandbox`](/command/sandbox)
