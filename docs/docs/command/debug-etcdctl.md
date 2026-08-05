---
title: "miren debug etcdctl"
sidebar_label: "debug etcdctl"
description: "Run etcdctl against Miren's embedded etcd"
---

# miren debug etcdctl

Run etcdctl against Miren's embedded etcd

Place Miren's `-n`/`--namespace` and `--socket` flags before the etcdctl arguments. Once the etcdctl arguments begin, all remaining flags are passed through to etcdctl unchanged.

## Usage

```bash
miren debug etcdctl [args...] [flags]
```

## Flags

- `--namespace, -n` — containerd namespace (default: `miren`)
- `--socket` — path to containerd socket

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show endpoint status:**

```bash
miren debug etcdctl endpoint status --write-out=table
```

**List every key:**

```bash
miren debug etcdctl get / --prefix --keys-only
```

## See also

- [`miren debug`](/command/debug)
