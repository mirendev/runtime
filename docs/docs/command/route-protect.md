---
title: "miren route protect"
sidebar_label: "route protect"
description: "Protect an HTTP route with an identity provider"
---

# miren route protect

Protect an HTTP route with an identity provider

## Usage

```bash
miren route protect <host> [flags]
```

## Arguments

- `host` — Hostname for the route (e.g., example.com); omit and pass --default for the default route

## Flags

- `--claim-header` — Claim to header mapping in format 'claim:header' (e.g., 'email:X-User-Email')
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--default` — Protect the default route (instead of a hostname)
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--provider` — Name of the identity provider

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Protect a route with an OIDC provider:**

```bash
miren route protect example.com --provider my-google-oidc --claim-header email:X-User-Email
```

**Protect a route with a password:**

```bash
miren route protect example.com --provider my-pw
```

**Protect the default route:**

```bash
miren route protect --default --provider my-google-oidc
```

## See also

- [`miren route`](/command/route)
