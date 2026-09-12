---
title: "miren sandbox"
sidebar_label: "sandbox"
description: "Sandbox management commands"
---

# miren sandbox

Sandbox management commands

Sandboxes are the underlying execution environments for your applications. Most of the time you'll work with apps directly, but these commands are useful for debugging and advanced use cases.

## Usage

```bash
miren sandbox [flags]
```

## Subcommands

- [`miren sandbox delete`](./sandbox-delete.md) — Delete a dead sandbox
- [`miren sandbox exec`](./sandbox-exec.md) — Open interactive shell in an existing sandbox
- [`miren sandbox inspect`](./sandbox-inspect.md) — Show one sandbox's resource usage and failure history
- [`miren sandbox list`](./sandbox-list.md) — List sandboxes (excludes dead by default)
- [`miren sandbox stop`](./sandbox-stop.md) — Stop a sandbox
