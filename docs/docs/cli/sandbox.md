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

Execute a command or open an interactive shell inside a running sandbox.

This command connects to an existing sandbox and runs a command inside it. Unlike `miren app run` which creates a new ephemeral sandbox, this connects to a sandbox that's already running (typically one serving production traffic).

### Usage

```bash
miren sandbox exec --id <sandbox-id> [-- command [args...]]
```

### Flags

- `--id` - Sandbox ID (required). Find sandbox IDs with `miren sandbox list`

### Examples

```bash
# Open an interactive shell in a sandbox
miren sandbox exec --id sandbox/myapp-web-abc123

# Run a simple command
miren sandbox exec --id sandbox/myapp-web-abc123 -- ls -la

# Check running processes
miren sandbox exec --id sandbox/myapp-web-abc123 -- ps aux

# View environment variables
miren sandbox exec --id sandbox/myapp-web-abc123 -- env

# Tail application logs
miren sandbox exec --id sandbox/myapp-web-abc123 -- tail -f /var/log/app.log
```

### Finding Sandbox IDs

Use `miren sandbox list` to find the ID of a running sandbox:

```bash
$ miren sandbox list
ID                          APP       SERVICE   STATUS    NODE
sandbox/myapp-web-abc123    myapp     web       RUNNING   node-1
sandbox/myapp-web-def456    myapp     web       RUNNING   node-2
```

:::warning
When you exec into a production sandbox, you're connecting to a live instance that may be serving traffic. Be careful with commands that could affect the running application.
:::

:::tip
For debugging or one-off tasks without affecting production, use `miren app run` to create an isolated ephemeral sandbox instead.
:::

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
