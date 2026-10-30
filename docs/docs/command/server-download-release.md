---
title: "miren server download-release"
sidebar_label: "server download-release"
description: "Download and extract miren release"
---

# miren server download-release

Download and extract miren release

## Usage

```bash
miren server download-release [flags]
```

## Flags

- `--branch, -b` — Branch name to download
- `--force, -f` — Force download even if release directory exists
- `--global, -g` — Install globally to /var/lib/miren/release
- `--output, -o` — Custom output directory

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Download the latest release:**

```bash
miren server download-release
```

## See also

- [`miren server`](./server.md)
