---
title: "miren route tls-check"
sidebar_label: "route tls-check"
description: "Ask an app before issuing certificates for names under its route"
---

# miren route tls-check

Ask an app before issuing certificates for names under its route

## Usage

```bash
miren route tls-check [host] [path] [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., *.example.com)
- `path` — Path on the app to ask before issuing a certificate (e.g., /tls-check); omit to show the current value

## Flags

- `--clear` — Remove the check so names under the route only get certificates as live ephemeral deploys
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Let a wildcard app vouch for its own subdomains:**

```bash
miren route tls-check '*.example.com' /tls-check
```

**Show the current check for a route:**

```bash
miren route tls-check '*.example.com'
```

**Remove the check:**

```bash
miren route tls-check '*.example.com' --clear
```

## See also

- [`miren route`](./route.md)
