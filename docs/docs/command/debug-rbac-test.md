---
title: "miren debug rbac test"
sidebar_label: "debug rbac test"
description: "Test RBAC evaluation with fetched rules"
---

# miren debug rbac test

Test RBAC evaluation with fetched rules

## Usage

```bash
miren debug rbac test [flags]
```

## Flags

- `--action, -a` — Action to test
- `--dir, -d` — Registration directory (default: `/var/lib/miren/server`)
- `--group, -g` — Groups to test with
- `--resource, -r` — Resource to test

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug rbac`](./debug-rbac.md)
