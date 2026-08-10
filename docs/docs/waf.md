---
title: Web Application Firewall (WAF)
description: Protect your apps from common web attacks with built-in OWASP WAF filtering at the routing layer.
keywords: [waf, web application firewall, security, owasp, sql injection, xss, coraza, crs]
---

import CliCommand from '@site/src/components/CliCommand';

# Web Application Firewall (WAF)

Miren includes a built-in WAF that filters malicious HTTP requests before they reach your app. It uses the [OWASP Core Rule Set](https://coreruleset.org/) (CRS) to detect common attacks like SQL injection, cross-site scripting (XSS), and path traversal.

WAF is configured per route. Enable it and all requests to that route are inspected — malicious requests get a `403 Forbidden` response, clean requests pass through normally. No changes to your app are needed.

:::note[What this doesn't cover]
The WAF inspects request content for attack payloads (SQL injection, XSS, command injection, path traversal). It does **not** rate-limit, fingerprint bots, or block reconnaissance scans (e.g., probes for `/wp-admin/` or `/xmlrpc.php` on non-WordPress sites). For that kind of filtering, use an upstream proxy like Cloudflare in front of your route.
:::

## Minimum working example

<CliCommand context="client">
```miren
miren route waf myapp.example.com
```
</CliCommand>

That's the entire setup — WAF is enabled at paranoia level 1 (the default), which catches well-known attack patterns with minimal false positives. Malicious requests to the route now get a `403 Forbidden`; clean requests pass through unchanged.

To use a higher paranoia level:

<CliCommand context="client">
```miren
miren route waf myapp.example.com --level 2
```
</CliCommand>

## Disabling WAF

<CliCommand context="client">
```miren
miren route waf myapp.example.com --disable
```
</CliCommand>

## Paranoia Levels

The paranoia level controls how aggressively the WAF inspects requests. Higher levels catch more attacks but may also flag legitimate requests as malicious (false positives).

| Level | Description | Use case |
|-------|-------------|----------|
| **1** | Baseline — catches obvious attacks (SQL injection, XSS, etc.) with very few false positives | Most apps (recommended default) |
| **2** | Elevated — additional rules for less common attack patterns | Apps handling sensitive data |
| **3** | High — stricter matching, may flag unusual but legitimate requests | High-security environments |
| **4** | Maximum — strictest rule set, highest false positive rate | Specialized security requirements |

Start with level 1. If you need tighter security, increase the level and monitor for false positives.

## What Gets Blocked

The OWASP CRS protects against the most common web attack categories:

- **SQL injection** — `?id=1 OR 1=1--`
- **Cross-site scripting (XSS)** — `?q=<script>alert(1)</script>`
- **Path traversal** — `/../../etc/passwd`
- **Remote code execution** — shell command injection in parameters
- **Protocol violations** — malformed HTTP requests
- **Request smuggling** — ambiguous content-length/transfer-encoding

When a request is blocked, the client receives a `403 Forbidden` response. The original request never reaches your app.

## Checking WAF Status

WAF status appears in both `route show` and `route list`:

<CliCommand context="client">
```miren
miren route show myapp.example.com
```
</CliCommand>

The output includes a `WAF Level` field when WAF is enabled. `route list` shows a `WAF` column with the level number or `-` when disabled.

## Default Route

WAF works with the default route too:

<CliCommand context="client">
```miren
miren route waf --default --level 1
miren route waf --default --disable
```
</CliCommand>

## JSON Output

Both `route waf` and `route show` support `--format json` for scripting:

<CliCommand context="client">
```miren
miren route waf myapp.example.com --format json
miren route show myapp.example.com --format json
```
</CliCommand>

The JSON output includes a `waf_level` field (integer, 0 when disabled).

## How It Works

WAF inspection runs in the HTTP ingress layer, before any other middleware (including OIDC authentication). The processing order for an incoming request is:

1. **WAF** — inspect the request against OWASP CRS rules
2. **OIDC** — authenticate the user (if route protection is configured)
3. **Proxy** — forward the request to the app sandbox

Miren uses [Coraza](https://coraza.io/), an open-source WAF engine compatible with ModSecurity rules, with the full OWASP Core Rule Set embedded. WAF engines are created per paranoia level and cached — there's no per-request initialization overhead.

:::warning[Request body size limit]
Request bodies up to 10 MB are inspected. Requests with bodies exceeding this limit are rejected with HTTP 413.
:::

## See Also

- [CLI: `miren route waf`](/command/route-waf) — enable or disable WAF on a route
- [Traffic Routing](/traffic-routing) — how routes work
- [Protecting Routes](/route-protect) — OIDC authentication for routes
