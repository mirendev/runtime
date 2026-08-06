---
title: "miren debug saga show"
sidebar_label: "debug saga show"
description: "Show a saga execution in detail"
---

# miren debug saga show

Show a saga execution in detail

Shows one saga execution in full: its status, its initial inputs, and every action it ran with timing, undo state, and output.

:::note[Output details]
Action outputs are truncated by default so a long saga stays readable. Pass `--full` to print them whole. `--format json` always carries them in full, since a partial record is worse than a large one for anything parsing it.

Where a saga stopped is the last action listed. This shows the actions that ran, not the complete set the definition declares, since the saga definitions live in the server process and are not exposed over the API.
:::

```bash
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY --full
miren debug saga show saga/sg-4TzP9hQ2mKdX8vNfR3wLbY --format json
```

## Usage

```bash
miren debug saga show [args...] [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--full` — Print complete action outputs instead of truncating them (JSON always includes them)
- `--id, -i` — Saga execution ID
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug saga`](/command/debug-saga)
