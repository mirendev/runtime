---
title: "miren auth provider add github"
sidebar_label: "auth provider add github"
description: "Add a GitHub identity provider"
---

# miren auth provider add github

Add a GitHub identity provider

## Usage

```bash
miren auth provider add github <name> [flags]
```

## Arguments

- `name` — Name for this identity provider

## Flags

- `--client-id` — GitHub OAuth app client ID
- `--client-secret` — GitHub OAuth app client secret
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--org` — GitHub org restriction (repeatable). Use "name" for any-member, or "name:team1,team2" to require team membership and populate X-User-Groups.
- `--update` — Overwrite an existing provider with the same name (rotates client secret)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Add a GitHub provider scoped to a team:**

```bash
miren auth provider add github my-gh \
  --client-id $CLIENT_ID \
  --client-secret $CLIENT_SECRET \
  --org mirendev:platform,eng
```

## See also

- [`miren auth provider add`](./auth-provider-add.md)
