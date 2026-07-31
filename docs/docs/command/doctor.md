---
title: "miren doctor"
sidebar_label: "doctor"
description: "Diagnose miren environment and connectivity"
---

# miren doctor

Diagnose miren environment and connectivity

## Usage

```bash
miren doctor [flags]
```

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Run all diagnostics:**

```bash
miren doctor
```

## Subcommands

- [`miren doctor auth`](/command/doctor-auth) — Check authentication and user information
- [`miren doctor config`](/command/doctor-config) — Check configuration files
- [`miren doctor server`](/command/doctor-server) — Check server health and connectivity
