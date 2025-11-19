---
sidebar_position: 3
---

# Sandbox Commands

Commands for managing Miren sandboxes (isolated execution environments for your apps).

:::info
Sandboxes are the underlying execution environments for your applications. Most of the time you'll work with apps directly, but these commands are useful for debugging and advanced use cases.
:::

## miren sandbox list

List all sandboxes.

### Usage

```bash
miren sandbox list [flags]
```

### Examples

```bash
# List all sandboxes
miren sandbox list
```

## miren sandbox exec

Execute a command inside a running sandbox.

### Usage

```bash
miren sandbox exec <id> -- <command> [args...]
```

The `--` separator is required to separate Miren flags from the command to execute.

### Examples

```bash
# Run a simple command
miren sandbox exec sb-abc123 -- ls -la

# Start an interactive shell
miren sandbox exec sb-abc123 -- /bin/bash

# Check running processes
miren sandbox exec sb-abc123 -- ps aux
```

## miren sandbox stop

Stop a running sandbox.

### Usage

```bash
miren sandbox stop <id> [flags]
```

### Examples

```bash
# Stop a sandbox
miren sandbox stop sb-abc123
```

## miren sandbox delete

Delete a dead sandbox.

### Usage

```bash
miren sandbox delete <id> [flags]
```

### Examples

```bash
# Delete a dead sandbox
miren sandbox delete sb-abc123
```

## miren sandbox metrics

Get metrics from a sandbox.

### Usage

```bash
miren sandbox metrics <id> [flags]
```

### Examples

```bash
# Get sandbox metrics
miren sandbox metrics sb-abc123
```

## Next Steps

- [App Commands](/cli/app) - Manage applications (the usual workflow)
- [CLI Reference](/cli-reference) - See all available commands
