---
title: "miren route"
sidebar_label: "route"
description: "Manage HTTP routes"
---

# miren route

Manage HTTP routes

## Usage

```bash
miren route [flags]
```

## Subcommands

- [`miren route delete`](./route-delete.md) — Remove an HTTP route
- [`miren route down`](./route-down.md) — Put an HTTP route into maintenance
- [`miren route list`](./route-list.md) — List all HTTP routes
- [`miren route protect`](./route-protect.md) — Protect an HTTP route with an identity provider
- [`miren route remove`](./route-remove.md) — Deprecated: use 'miren route delete'
- [`miren route set`](./route-set.md) — Create or update an HTTP route
- [`miren route set-default`](./route-set-default.md) — Set an app as the default route
- [`miren route show`](./route-show.md) — Show details of an HTTP route
- [`miren route timeout`](./route-timeout.md) — Override the ingress request timeout for an HTTP route
- [`miren route tls-check`](./route-tls-check.md) — Ask an app before issuing certificates for names under its route
- [`miren route unprotect`](./route-unprotect.md) — Remove identity-provider protection from an HTTP route
- [`miren route unset-default`](./route-unset-default.md) — Remove the default route
- [`miren route up`](./route-up.md) — Bring an HTTP route out of maintenance
- [`miren route waf`](./route-waf.md) — Manage WAF protection on an HTTP route
