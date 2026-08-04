---
title: "miren runner token create"
sidebar_label: "runner token create"
description: "Create a join token for a runner"
---

# miren runner token create

Create a join token for a runner

## Usage

```bash
miren runner token create [flags]
```

## Flags

- `--addr, -a` — Override coordinator address baked into the token
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--expires, -e` — Hours until the invite expires (default: `1`)
- `--labels, -l` — Labels to apply to the runner (key=value format)
- `--name, -n` — Human-readable name for this invite
- `--reusable, -r` — Create a reusable invite (not consumed on use)
- `--ttl` — Time-to-live (e.g. 24h, 7d, 2w). Overrides --expires

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Create a one-time join token:**

```bash
miren runner token create
```

**Create a reusable token for automation:**

```bash
miren runner token create --reusable --name infra-terraform --ttl 0
```

**Create a token with a specific coordinator address:**

```bash
miren runner token create --addr 10.0.0.5:8443
```

## See also

- [`miren runner token`](/command/runner-token)
