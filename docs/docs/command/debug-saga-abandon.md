---
title: "miren debug saga abandon"
sidebar_label: "debug saga abandon"
description: "Give up a saga execution the server refused to resume (break-glass)"
---

# miren debug saga abandon

Give up a saga execution the server refused to resume (break-glass)

Gives up a saga execution the server has refused to resume, marking it failed **without** undoing the work it already did. It lists the actions whose work it will leave behind before asking you to confirm, so you know what to clean up by hand.

You should almost never need this. The server refuses to resume a saga only when its definition cannot vouch for the one the saga was started under, and the normal fix is to run a release that can still resume it, which lets the saga finish or roll back cleanly. Reach for abandon only when that is not an option.

Only blocked executions can be abandoned. One that is pending, running, or undoing without a block is still being driven or will be picked up by recovery, and abandoning it would skip a compensation that was going to happen on its own. A saga whose child saga is still in flight can't be abandoned either: abandon the blocked child first, which lists what it leaves behind, and the parent unwinds on its own the next time it is driven. After abandoning, a sandbox's saga is retried from scratch on its next reconcile, and a saga with a generated name is cleaned up by saga retention. Addon provisioning is the exception: an abandoned provisioning saga leaves its addon in an error state, so destroy the addon with `miren addon destroy` and add it again.

```bash
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
miren debug saga abandon saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
```

## Usage

```bash
miren debug saga abandon [args...] [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--force, -f` — Skip confirmation prompt
- `--id, -i` — Saga execution ID

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug saga`](./debug-saga.md)
