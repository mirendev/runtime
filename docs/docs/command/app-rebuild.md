---
title: "miren app rebuild"
sidebar_label: "app rebuild"
description: "Rebuild and deploy the currently deployed source"
---

# miren app rebuild

Rebuild and deploy the currently deployed source

Rebuild the active version using source retained on the server, refreshing mutable base image tags for stackbuild and Dockerfile builds. No local files are uploaded. The current runtime configuration is preserved, deploy tasks run, and the command waits for the new version to become healthy. Apps deployed before source retention was available must be deployed once with an updated server first. Digest-pinned base images remain pinned.

## Usage

```bash
miren app rebuild [flags]
```

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--message, -m` — Description of this rebuild

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Refresh an app's base images:**

```bash
miren app rebuild -a myapp
```

## See also

- [`miren app`](./app.md)
