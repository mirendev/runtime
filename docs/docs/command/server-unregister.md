---
title: "miren server unregister"
sidebar_label: "server unregister"
description: "Detach this cluster from miren.cloud"
---

# miren server unregister

Detach this cluster from miren.cloud

## Usage

```bash
miren server unregister [flags]
```

## Flags

- `--force, -f` — Skip the confirmation prompt
- `--local-only` — Clear local registration without telling miren.cloud
- `--output, -o` — Directory holding the registration (default: `/var/lib/miren/server`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Unregister from cloud:**

```bash
miren server unregister
```

**Clear local registration when the cloud entry is already gone:**

```bash
miren server unregister --local-only
```

## See also

- [`miren server`](/command/server)
