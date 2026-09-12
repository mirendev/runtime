---
title: "miren download release"
sidebar_label: "download release"
description: "Download and extract miren release"
---

# miren download release

Download and extract miren release

## Usage

```bash
miren download release [flags]
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
miren download release
```

## See also

- [`miren download`](./download.md)
