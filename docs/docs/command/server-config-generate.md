---
title: "miren server config generate"
sidebar_label: "server config generate"
description: "Generate a server configuration file from current settings"
---

# miren server config generate

Generate a server configuration file from current settings

## Usage

```bash
miren server config generate [flags]
```

## Flags

- `--defaults, -d` — Generate config with default values
- `--mode, -m` — Server mode: standalone (default), distributed (experimental) (default: `standalone`)
- `--output, -o` — Output file path (defaults to stdout)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Generate config with defaults:**

```bash
miren server config generate --defaults
```

**Generate and save to file:**

```bash
miren server config generate --defaults --output server.toml
```

## See also

- [`miren server config`](./server-config.md)
