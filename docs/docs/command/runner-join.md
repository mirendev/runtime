---
title: "miren runner join"
sidebar_label: "runner join"
description: "Join this machine to a coordinator as a runner"
---

# miren runner join

Join this machine to a coordinator as a runner

## Usage

```bash
miren runner join <tokenarg> [flags]
```

## Arguments

- `tokenarg` — Join token from 'miren runner token create'

## Flags

- `--config` — Path to save runner config (default: `/var/lib/miren/runner/config.yaml`)
- `--coordinator, -c` — Override coordinator address from the token
- `--labels` — Additional labels for the runner (key=value)
- `--listen, -l` — Address this runner will listen on
- `--name` — Human-readable name for this runner (defaults to hostname)
- `--runner-id` — Specific runner ID to use (for reconnecting)
- `--token` — Enrollment token (or pass as positional arg / via stdin)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Join using a token:**

```bash
miren runner join mren_...
```

**Join with coordinator address override:**

```bash
miren runner join mren_... --coordinator 10.0.0.5:8443
```

## See also

- [`miren runner`](/command/runner)
