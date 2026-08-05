---
title: "miren debug entity list"
sidebar_label: "debug entity list"
description: "List entities"
---

# miren debug entity list

List entities

## Usage

```bash
miren debug entity list [flags]
```

## Flags

- `--address` — Address to listen on (default: `localhost:8443`)
- `--after` — Resume from the cursor printed by a previous page
- `--attribute, -a` — Attribute to filter by
- `--cluster, -C` — Cluster name
- `--columns` — Comma-separated fields to use as table columns
- `--config` — Path to the config file
- `--detail, -l` — Show each entity in full instead of one line each
- `--expand` — Show nested components in full
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--kind, -k` — Kind of entity to filter by
- `--limit, -n` — Max entities to show, 0 for all (default: `50`)
- `--max-value-len` — Elide values longer than this, 0 for no limit (default: `120`)
- `--value, -V` — Value to filter by

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug entity`](/command/debug-entity)
