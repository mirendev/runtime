---
title: "miren route timeout"
sidebar_label: "route timeout"
description: "Override the ingress request timeout for an HTTP route"
---

# miren route timeout

Override the ingress request timeout for an HTTP route

## Usage

```bash
miren route timeout <host> <timeout> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com); omit and pass --default for the default route
- `timeout` — Request timeout as a duration (e.g., 10m, 300s); omit to show the current value

## Flags

- `--clear` — Remove the override so the route uses the server default
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Apply to the default route (instead of a hostname)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Give a long-poll route a 10-minute timeout:**

```bash
miren route timeout example.com 10m
```

**Show the current timeout for a route:**

```bash
miren route timeout example.com
```

**Set a timeout on the default route:**

```bash
miren route timeout --default 5m
```

**Go back to the server default timeout:**

```bash
miren route timeout example.com --clear
```

## See also

- [`miren route`](/command/route)
