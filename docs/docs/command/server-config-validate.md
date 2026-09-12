---
title: "miren server config validate"
sidebar_label: "server config validate"
description: "Validate a server configuration file"
---

# miren server config validate

Validate a server configuration file

## Usage

```bash
miren server config validate [flags]
```

## Flags

- `--file, -f` — Configuration file to validate

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Validate a config file:**

```bash
miren server config validate --file server.toml
```

## See also

- [`miren server config`](./server-config.md)
