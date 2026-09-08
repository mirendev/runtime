---
title: "miren debug entity"
sidebar_label: "debug entity"
description: "Entity store debug commands"
---

# miren debug entity

Entity store debug commands

Entities are the low-level objects stored in Miren's entity system. Most users won't need to use these commands directly. They're primarily useful for debugging and advanced use cases.

## What are Entities?

Entities are flexible metadata objects stored in Miren's etcd-backed entity store. Everything in Miren is an entity:

- **Apps** - Application definitions
- **Sandboxes** - Running containers
- **Versions** - Immutable app configurations
- **Clusters** - Cluster registrations
- **Users** - User accounts

## Usage

```bash
miren debug entity [flags]
```

## Subcommands

- [`miren debug entity create`](./debug-entity-create.md) — Create a new entity
- [`miren debug entity delete`](./debug-entity-delete.md) — Delete an entity
- [`miren debug entity ensure`](./debug-entity-ensure.md) — Ensure an entity exists
- [`miren debug entity get`](./debug-entity-get.md) — Get an entity
- [`miren debug entity list`](./debug-entity-list.md) — List entities
- [`miren debug entity patch`](./debug-entity-patch.md) — Patch an existing entity
- [`miren debug entity put`](./debug-entity-put.md) — Put an entity
- [`miren debug entity replace`](./debug-entity-replace.md) — Replace an existing entity

## See also

- [`miren debug`](./debug.md)
