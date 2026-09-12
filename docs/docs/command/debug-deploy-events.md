---
title: "miren debug deploy-events"
sidebar_label: "debug deploy-events"
description: "Render a 'miren deploy --format jsonl' stream as readable output"
---

# miren debug deploy-events

Render a 'miren deploy --format jsonl' stream as readable output

A deploy run with `--format jsonl` writes one JSON object per line and nothing a person would want to read. This command turns that stream back into the output the deploy would have printed: the phase summaries, build steps, health verdict, routes, and final status. Point it at a saved file, or pipe a live deploy through it.

Build step log lines are hidden unless `--build-logs` is set; `--timestamps` prefixes every line with the time since the first event, which is the quickest way to see where a slow deploy spent its time. Lines that are not JSON (stderr text mixed into the same file, say) are shown as they are.

## Usage

```bash
miren debug deploy-events <file> [flags]
```

## Arguments

- `file` — JSONL file written by 'miren deploy --format jsonl' (default: stdin)

## Flags

- `--build-logs` — Show the log lines of every build step, not just the steps
- `--timestamps, -t` — Prefix each line with the time elapsed since the first event

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Read a captured deploy:**

```bash
miren debug deploy-events deploy.jsonl
```

**With elapsed times and build output:**

```bash
miren debug deploy-events -t --build-logs deploy.jsonl
```

**Watch a deploy live:**

```bash
miren deploy --format jsonl | tee deploy.jsonl | miren debug deploy-events
```

## See also

- [`miren debug`](./debug.md)
