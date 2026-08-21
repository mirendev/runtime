---
title: "miren route up"
sidebar_label: "route up"
description: "Bring an HTTP route out of maintenance"
---

# miren route up

Bring an HTTP route out of maintenance

Brings a route out of maintenance and back to serving normally.

This clears the whole maintenance record, including the reason and the operator who set it — a stale "Upgrading the database" hanging off a healthy route is a trap for whoever reads it next.

Running this on a route that is already serving succeeds and says so, rather than erroring.

## Usage

```bash
miren route up <host> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com); omit and pass --default for the default route

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Bring the default route back (instead of a hostname)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Bring a route back:**

```bash
miren route up example.com
```

**Bring the default route back:**

```bash
miren route up --default
```

## See also

- [`miren route`](/command/route)
