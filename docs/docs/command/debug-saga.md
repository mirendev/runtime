---
title: "miren debug saga"
sidebar_label: "debug saga"
description: "Saga execution debug commands"
---

# miren debug saga

Saga execution debug commands

Sagas are Miren's mechanism for multi-step operations that have to either finish or unwind cleanly, like provisioning an addon or building and deploying an app. Each saga execution records what it ran, in what order, and what each action returned, so a saga that wedges leaves behind a trail you can read.

These commands read that trail. The underlying records are also visible through `miren debug entity list -k saga`, but the interesting fields are stored as JSON blobs, so that view shows you base64 rather than what happened.

## Usage

```bash
miren debug saga [flags]
```

## Subcommands

- [`miren debug saga list`](/command/debug-saga-list) — List saga executions
- [`miren debug saga show`](/command/debug-saga-show) — Show a saga execution in detail

## See also

- [`miren debug`](/command/debug)
