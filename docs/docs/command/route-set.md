---
title: "miren route set"
sidebar_label: "route set"
description: "Create or update an HTTP route"
---

# miren route set

Create or update an HTTP route

## Usage

```bash
miren route set <host> <appname> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com or *.example.com)
- `appname` — Application name to route to

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--service` — HTTP-capable app service to route to (default: `web`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Route a domain to an app:**

```bash
miren route set example.com myapp
```

## See also

- [`miren route`](/command/route)
