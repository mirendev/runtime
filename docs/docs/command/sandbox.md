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

- [`miren sandbox delete`](/command/sandbox-delete) — Delete a dead sandbox
- [`miren sandbox exec`](/command/sandbox-exec) — Open interactive shell in an existing sandbox
- [`miren sandbox inspect`](/command/sandbox-inspect) — Show one sandbox's resource usage and failure history
- [`miren sandbox list`](/command/sandbox-list) — List sandboxes (excludes dead by default)
- [`miren sandbox stop`](/command/sandbox-stop) — Stop a sandbox
