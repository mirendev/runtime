---
title: "miren secret"
sidebar_label: "secret"
description: "Secret store management commands"
---

# miren secret

Secret store management commands

## Usage

```bash
miren secret [flags]
```

## Subcommands

- [`miren secret destroy`](./secret-destroy.md) — Permanently delete a version's value
- [`miren secret disable`](./secret-disable.md) — Stop a version from resolving
- [`miren secret enable`](./secret-enable.md) — Let a disabled version resolve again
- [`miren secret keyring`](./secret-keyring.md) — Show the cluster keyring and any rotation in flight
- [`miren secret list`](./secret-list.md) — List stored secrets
- [`miren secret rotate-key`](./secret-rotate-key.md) — Rotate the cluster key that encrypts stored secrets
- [`miren secret set`](./secret-set.md) — Store a secret value
- [`miren secret versions`](./secret-versions.md) — Show a secret's versions
