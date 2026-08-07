---
title: "miren secret rotate-key"
sidebar_label: "secret rotate-key"
description: "Rotate the cluster key that encrypts stored secrets"
---

# miren secret rotate-key

Rotate the cluster key that encrypts stored secrets

Rotates the cluster key that encrypts stored secrets, without waiting for the automatic age policy. This is what an incident calls for.

Rotation is not disruptive. A new key is minted and new writes use it immediately, then existing versions are re-wrapped onto it in the background — only the small wrapped data key is rewritten, never the secret values themselves, so the cost does not depend on how large your secrets are. Every version stays readable throughout, and the old key is retired only once nothing references it.

Watch progress with `miren secret keyring`.

:::note[Your secrets do not change]
Rotating the cluster key re-encrypts how values are stored. It does not change any secret's value or mint new versions, so nothing referencing a secret needs redeploying.
:::

## Usage

```bash
miren secret rotate-key [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Rotate now, without waiting for the age policy:**

```bash
miren secret rotate-key
```

## See also

- [`miren secret`](/command/secret)
