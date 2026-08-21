---
title: "miren route down"
sidebar_label: "route down"
description: "Put an HTTP route into maintenance"
---

# miren route down

Put an HTTP route into maintenance

Puts a route into maintenance. Visitors get HTTP 503 and a holding page instead of reaching the app; nothing else changes.

Maintenance is a routing decision and only a routing decision. The app's sandboxes keep running, so `miren app run` and one-shot migrations still work during the window — which is the whole point of taking traffic off the route first. Other hostnames pointing at the same app keep serving, and internal service-to-service calls are unaffected.

`--reason` is shown to visitors, so write it for them rather than for your team. `--back-at` accepts a clock time (`15:00`), a duration (`30m`), or an RFC 3339 timestamp; it drives both the holding page copy and the `Retry-After` header that crawlers and uptime monitors read.

:::note[Ephemeral preview URLs are covered]
Preview subdomains resolve through their base route, so taking `app.example.com` down also holds `feat-x.app.example.com`. Previews share the app's database, so a migration window that left them serving would not be a window at all.
:::

## Usage

```bash
miren route down <host> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com); omit and pass --default for the default route

## Flags

- `--back-at` — When the route is expected back: a clock time in your own timezone (15:00), a duration (30m), or an RFC 3339 timestamp
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Take the default route down (instead of a hostname)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--reason` — Explanation shown to visitors on the holding page
- `--yes, -y` — Skip the confirmation prompt for --default

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Take a route down with an explanation:**

```bash
miren route down example.com --reason "Upgrading the database"
```

**Take a route down with an expected return time:**

```bash
miren route down example.com --reason "DB migration" --back-at 15:00
```

**Take the default route down:**

```bash
miren route down --default --reason "Cluster upgrade"
```

## See also

- [`miren route`](/command/route)
