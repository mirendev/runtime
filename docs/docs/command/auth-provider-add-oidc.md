---
title: "miren auth provider add oidc"
sidebar_label: "auth provider add oidc"
description: "Add an OIDC identity provider"
---

# miren auth provider add oidc

Add an OIDC identity provider

## Usage

```bash
miren auth provider add oidc <name> [flags]
```

## Arguments

- `name` — Name for this identity provider

## Flags

- `--client-id` — OAuth2 client ID
- `--client-secret` — OAuth2 client secret
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--provider-url` — OIDC provider URL (e.g., https://accounts.google.com)
- `--scope` — OAuth2 scopes (can be specified multiple times)
- `--update` — Overwrite an existing provider with the same name (rotates client secret)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Add a Google OIDC provider:**

```bash
miren auth provider add oidc my-google \
  --provider-url https://accounts.google.com \
  --client-id $CLIENT_ID \
  --client-secret $CLIENT_SECRET \
  --scope email --scope profile
```

## See also

- [`miren auth provider add`](./auth-provider-add.md)
