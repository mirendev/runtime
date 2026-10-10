---
title: "miren session create"
sidebar_label: "session create"
description: "Create a Session from an app service"
---

# miren session create

Create a Session from an app service

## Usage

```bash
miren session create [flags]
```

## Flags

- `--group` — Optional opaque sharing key within the app and service
- `--idle-timeout` — Park after continuously idle for this duration; 0 disables (default: `5m`)
- `--max-sessions-per-sandbox` — Shared host capacity (greater than one enables sharing) (default: `1`)
- `--name` — Stable name for the Session; generated if omitted
- `--service, -s` — Service to run (default: `web`)

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

**Dedicated Session:**

```bash
miren session create -a myapp --name customer-1
```

**Shared Session:**

```bash
miren session create -a myapp --name customer-2 --max-sessions-per-sandbox 4
```

## See also

- [`miren session`](./session.md)
