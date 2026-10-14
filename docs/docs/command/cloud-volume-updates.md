---
title: "miren cloud volume updates"
sidebar_label: "cloud volume updates"
description: "List a cloud volume's snapshots and update artifacts"
---

# miren cloud volume updates

List a cloud volume's snapshots and update artifacts

## Usage

```bash
miren cloud volume updates [flags]
```

## Flags

- `--cursor` — Cursor from the previous response (kind:ordering_key)
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--kind` — Filter update kind
- `--limit` — Page size (Cloud default 50, range 1-200) (default: `0`)
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)
- `--volume` — Cloud volume ID

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud volume`](./cloud-volume.md)
