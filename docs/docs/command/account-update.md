---
title: "miren account update"
sidebar_label: "account update"
description: "Update your Cloud profile, preserving omitted fields"
---

# miren account update

Update your Cloud profile, preserving omitted fields

## Usage

```bash
miren account update [flags]
```

## Flags

- `--avatar-url` — New avatar URL; omitted preserves it
- `--clear-avatar` — Remove your avatar
- `--email` — New email address; omitted preserves it
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--name` — New display name; omitted preserves it

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren account`](./account.md)
