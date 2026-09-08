---
title: "miren route waf"
sidebar_label: "route waf"
description: "Manage WAF protection on an HTTP route"
---

# miren route waf

Manage WAF protection on an HTTP route

## Usage

```bash
miren route waf <host> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com); omit and pass --default for the default route

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Apply to the default route (instead of a hostname)
- `--disable` — Disable WAF on the route
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--level` — OWASP CRS paranoia level (1-4) (default: `1`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Enable WAF on a route with default paranoia level:**

```bash
miren route waf example.com
```

**Enable WAF with a specific paranoia level:**

```bash
miren route waf example.com --level 2
```

**Enable WAF on the default route:**

```bash
miren route waf --default
```

**Disable WAF on a route:**

```bash
miren route waf example.com --disable
```

## See also

- [`miren route`](./route.md)
