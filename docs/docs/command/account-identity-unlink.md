---
title: "miren account identity unlink"
sidebar_label: "account identity unlink"
description: "Unlink a sign-in identity (cannot remove the last one)"
---

# miren account identity unlink

Unlink a sign-in identity (cannot remove the last one)

## Usage

```bash
miren account identity unlink [id] [flags]
```

## Arguments

- `id` — Provider's account ID from identity list (not a Cloud user XID)

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json
- `--provider` — Sign-in provider (choices: `github`, `google`)
- `--yes, -y` — Confirm this destructive operation without prompting

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren account identity`](./account-identity.md)
