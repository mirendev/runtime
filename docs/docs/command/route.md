---
title: "miren route"
sidebar_label: "route"
description: "List all HTTP routes"
---

# miren route

List all HTTP routes

## Usage

```bash
miren route [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**List all routes:**

```bash
miren route
```

## Subcommands

- [`miren route down`](./route-down.md) — Put an HTTP route into maintenance
- [`miren route list`](./route-list.md) — List all HTTP routes
- [`miren route protect`](./route-protect.md) — Protect an HTTP route with an identity provider
- [`miren route remove`](./route-remove.md) — Remove an HTTP route
- [`miren route set`](./route-set.md) — Create or update an HTTP route
- [`miren route set-default`](./route-set-default.md) — Set an app as the default route
- [`miren route show`](./route-show.md) — Show details of an HTTP route
- [`miren route timeout`](./route-timeout.md) — Override the ingress request timeout for an HTTP route
- [`miren route unprotect`](./route-unprotect.md) — Remove identity-provider protection from an HTTP route
- [`miren route unset-default`](./route-unset-default.md) — Remove the default route
- [`miren route up`](./route-up.md) — Bring an HTTP route out of maintenance
- [`miren route waf`](./route-waf.md) — Manage WAF protection on an HTTP route
