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

## Next Steps

- [CLI Reference](/cli-reference) - See all available commands
- [Getting Started](/getting-started) - Learn by example
