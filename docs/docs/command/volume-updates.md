---
title: "miren volume updates"
sidebar_label: "volume updates"
description: "List a cloud volume's snapshots and update artifacts"
---

# miren volume updates

List a cloud volume's snapshots and update artifacts

## Usage

```bash
miren volume updates [volume] [flags]
```

## Arguments

- `volume` — Cloud volume ID

## Flags

- `--cursor` — Cursor from the previous response (kind:ordering_key)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--kind` — Filter update kind
- `--limit` — Page size (Cloud default 50, range 1-200) (default: `0`)
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren volume`](./volume.md)
