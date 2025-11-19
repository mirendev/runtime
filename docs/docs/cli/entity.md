---
sidebar_position: 4
---

# Entity Commands

Commands for interacting with Miren's entity store.

:::info
Entities are the low-level objects stored in Miren's entity system. Most users won't need to use these commands directly. They're primarily useful for debugging and advanced use cases.
:::

## What are Entities?

Entities are flexible metadata objects stored in Miren's etcd-backed entity store. Everything in Miren is an entity:

- **Apps** - Application definitions
- **Sandboxes** - Running containers
- **Versions** - Immutable app configurations
- **Clusters** - Cluster registrations
- **Users** - User accounts

## miren entity list

List entities of a specific type.

### Usage

```bash
miren entity list <type> [flags]
```

### Examples

```bash
# List all apps
miren entity list app

# List all sandboxes
miren entity list sandbox
```

## miren entity get

Get a specific entity by type and name.

### Usage

```bash
miren entity get <type> <name> [flags]
```

### Examples

```bash
# Get an app entity
miren entity get app myapp

# Get a sandbox entity
miren entity get sandbox sb-abc123
```

## miren entity delete

Delete an entity.

### Usage

```bash
miren entity delete <type> <name> [flags]
```

### Examples

```bash
# Delete an entity
miren entity delete app myapp
```

## miren entity put

Put (create or update) an entity.

### Usage

```bash
miren entity put <type> <name> [flags]
```

:::warning
This is an advanced command. Use the higher-level commands like `miren deploy` instead when possible.
:::

## Next Steps

- [App Commands](/cli/app) - Higher-level app management
- [CLI Reference](/cli-reference) - See all available commands
