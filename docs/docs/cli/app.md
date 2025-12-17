---
sidebar_position: 2
---

# App Commands

Commands for managing Miren applications.

## miren app

Get information about an application.

### Usage

```bash
miren app [flags]
```

### Flags

- `--app, -a` - Application name (or use `$MIREN_APP` environment variable)
- `--dir, -d` - Directory to run from
- `--watch, -w` - Watch the app stats
- `--graph, -g` - Graph the app stats
- `--config-only` - Only show the configuration
- `--cluster, -C` - Cluster name
- `--format` - Output format (table, json)

### Examples

```bash
# Get info about the app in the current directory
miren app

# Get info about a specific app
miren app --app myapp

# Watch app stats
miren app --watch

# Show only configuration
miren app --config-only
```

## miren app list

List all applications.

### Usage

```bash
miren app list [flags]
# or
miren apps
```

### Examples

```bash
# List all apps in table format
miren app list

# List in JSON format
miren app list --format json

# Use the shorter alias
miren apps
```

## miren app status

Show current status of an application.

### Usage

```bash
miren app status [flags]
```

### Flags

- `--app, -a` - Application name
- `--cluster, -C` - Cluster name

### Examples

```bash
# Get app status
miren app status

# Get status for a specific app
miren app status --app myapp
```

## miren app history

Show deployment history for an application.

### Usage

```bash
miren app history [flags]
```

### Flags

- `--app, -a` - Application name
- `--cluster, -C` - Cluster name

### Examples

```bash
# View deployment history
miren app history

# View history for a specific app
miren app history --app myapp
```

## miren app delete

Delete an application and all its resources.

### Usage

```bash
miren app delete [flags]
```

### Flags

- `--app, -a` - Application name
- `--cluster, -C` - Cluster name

### Examples

```bash
# Delete an application
miren app delete --app myapp
```

## miren app run

Open an interactive shell in a new ephemeral sandbox for an application.

This command creates a temporary sandbox using your app's configuration (image, environment variables, working directory) and connects you to an interactive shell. The sandbox is automatically cleaned up when you exit.

This is useful for:
- Debugging application issues in an isolated environment
- Running one-off commands with your app's configuration
- Exploring the container filesystem
- Testing changes before deploying

### Usage

```bash
miren app run [flags] [-- command [args...]]
```

### Flags

- `--app, -a` - Application name (required)
- `--cluster, -C` - Cluster name

### Examples

```bash
# Open an interactive shell in your app's environment
miren app run

# Run a specific command
miren app run -- ls -la /app

# Start a Rails console
miren app run -- bin/rails console

# Run database migrations
miren app run -- bin/rails db:migrate

# Check Node.js dependencies
miren app run -- npm list

# Debug Python environment
miren app run -- python -c "import sys; print(sys.path)"
```

### How It Works

1. Miren fetches your app's active version configuration
2. Creates an ephemeral sandbox with the same image, environment variables, and working directory as your deployed app
3. Waits for the sandbox to become ready
4. Connects your terminal to an interactive shell inside the sandbox
5. Cleans up the sandbox automatically when you disconnect

:::tip
The ephemeral sandbox runs independently from your production sandboxes. Any changes you make (files created, packages installed) are discarded when you exit.
:::

:::note
If you need to run commands in an existing production sandbox, use `miren sandbox exec` instead.
:::

## Next Steps

- [CLI Reference](/cli-reference) - See all available commands
- [Getting Started](/getting-started) - Learn by example
