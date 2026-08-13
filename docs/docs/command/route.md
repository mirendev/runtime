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

- [`miren route list`](/command/route-list) — List all HTTP routes
- [`miren route protect`](/command/route-protect) — Protect an HTTP route with an identity provider
- [`miren route remove`](/command/route-remove) — Remove an HTTP route
- [`miren route set`](/command/route-set) — Create or update an HTTP route
- [`miren route set-default`](/command/route-set-default) — Set an app as the default route
- [`miren route show`](/command/route-show) — Show details of an HTTP route
- [`miren route timeout`](/command/route-timeout) — Override the ingress request timeout for an HTTP route
- [`miren route unprotect`](/command/route-unprotect) — Remove identity-provider protection from an HTTP route
- [`miren route unset-default`](/command/route-unset-default) — Remove the default route
- [`miren route waf`](/command/route-waf) — Manage WAF protection on an HTTP route
