---
title: "miren auth ci add"
sidebar_label: "auth ci add"
description: "Add a CI authentication binding to an application"
---

# miren auth ci add

Add a CI authentication binding to an application

## Usage

```bash
miren auth ci add [flags]
```

## Flags

- `--allowed-events` — Comma-separated event names to allow (default: push,workflow_dispatch)
- `--allowed-refs` — Glob pattern for allowed git refs
- `--description` — Human-readable description of this binding
- `--github` — GitHub owner/repo shorthand (sets issuer, subject, provider)
- `--issuer` — OIDC issuer URL
- `--subject` — Glob pattern for the token subject

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren auth ci`](/command/auth-ci)
