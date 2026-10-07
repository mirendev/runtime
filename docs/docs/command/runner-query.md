---
title: "miren runner query"
sidebar_label: "runner query"
description: "Run a Portal monitoring query on a runner and print the JSON result"
---

# miren runner query

Run a Portal monitoring query on a runner and print the JSON result

Use --reference to print query syntax and Miren source fields without connecting to a cluster.

## Usage

```bash
miren runner query [node] [expression] [flags]
```

## Arguments

- `node` — Runner to query (name, ID, or short ID)
- `expression` — Portal monitoring query expression (not SQL); quote expressions containing spaces

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--reference` — Print the offline query syntax and source reference

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Show the offline query reference:**

```bash
miren runner query --reference
```

**Inspect runner memory:**

```bash
miren runner query my-runner memory
```

**Sample memory usage over ten seconds:**

```bash
miren runner query my-runner "memory avg(used) over 10s every 1s"
```

## See also

- [`miren runner`](./runner.md)
