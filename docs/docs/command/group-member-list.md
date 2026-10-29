---
title: "miren group member-list"
sidebar_label: "group member-list"
description: "List access group members"
---

# miren group member-list

List access group members

## Usage

```bash
miren group member-list [group] [flags]
```

## Arguments

- `group` — Cloud group XID

## Flags

- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren group`](./group.md)
