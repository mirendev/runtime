---
title: "miren cloud invite update"
sidebar_label: "cloud invite update"
description: "Update an invite's expiry, use cap or groups (admin only)"
---

# miren cloud invite update

Update an invite's expiry, use cap or groups (admin only)

## Usage

```bash
miren cloud invite update [flags]
```

## Flags

- `--expires-in` — Invite lifetime: 1d, 7d, 30d or never (create defaults to 7d) (choices: `1d`, `7d`, `30d`, `never`)
- `--format` — Output format (text, json) (default: `text`)
- `--groups` — JSON array of group XIDs; [] clears groups on update
- `--identity, -i` — Cloud identity to use (required when more than one is configured)
- `--invite` — Invite link ID
- `--json` — Shorthand for --format json
- `--max-uses` — Maximum uses as a JSON integer, or null for unlimited; omitted leaves unchanged on update
- `--organization, -o` — Organization name or org- ID (defaults to your only organization)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren cloud invite`](./cloud-invite.md)
