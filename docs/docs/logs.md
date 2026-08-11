---
title: Logs
description: View and query application, build, sandbox, and system logs with the miren logs command.
keywords: [logs, logging, victoria logs, debugging, output, build logs]
---

import CliCommand from '@site/src/components/CliCommand';

# Logs

Miren captures logs from your applications, sandboxes, builds, and the system itself. Logs are stored in [VictoriaLogs](https://docs.victoriametrics.com/victorialogs/) and queryable through the `miren logs` command and its subcommands.

## Minimum working example

<CliCommand context="client">
```miren
# Last 100 lines for the current app
miren logs

# Follow in real time, filtered to errors
miren logs -f -g error
```
</CliCommand>

Add `-a myapp` to target a specific app. Build, sandbox, and system logs each have their own subcommand, covered below.

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `miren logs app` | Application logs (default when no subcommand is given) |
| `miren logs sandbox` | Logs from a specific sandbox instance |
| `miren logs build` | Build output for a specific app version |
| `miren logs system` | Miren server internal logs |

Running `miren logs` without a subcommand shows app logs (backward compatible).

## Viewing app logs

App logs are the most common use case. By default, `miren logs` shows the last 100 lines from your current app:

<CliCommand context="client">
```miren
# Show recent logs for the current app
miren logs

# Show logs for a specific app
miren logs app -a myapp

# Filter by service
miren logs app -a myapp --service web

# Show logs from the last 30 minutes
miren logs app --last 30m

# Follow logs in real-time
miren logs app -a myapp -f
```
</CliCommand>

## Build logs

View the output from a specific build to debug deployment failures:

<CliCommand context="client">
```miren
# Show build logs for the latest version
miren logs build -a myapp

# Show build logs for a specific version
miren logs build -a myapp VERSION
```
</CliCommand>

Replace `VERSION` with the version from `miren app history`.

## Sandbox logs

View logs from a specific sandbox instance, useful for debugging issues in individual containers:

<CliCommand context="client">
```miren
# Show logs for a specific sandbox
miren logs sandbox -s SANDBOX_ID
```
</CliCommand>

Use `miren sandbox list` to find sandbox IDs.

## System logs

View Miren server internal logs for debugging server-level behavior:

<CliCommand context="client">
```miren
miren logs system
```
</CliCommand>

System logs contain output from the Miren server process itself — controller reconciliation, RPC calls, sandbox lifecycle events, and other internal operations. Use these when debugging server behavior rather than application issues.

## Filtering with grep

Use `--grep` (or `-g`) to filter log output. The filter supports multiple syntax options:

| Syntax | Description | Example |
|--------|-------------|---------|
| `word` | Match logs containing "word" (case-insensitive) | `error` |
| `"phrase"` | Match logs containing exact phrase | `"connection failed"` |
| `'phrase'` | Match logs containing exact phrase (alternate) | `'connection failed'` |
| `/regex/` | Match logs matching regex pattern | `/err(or)?/` |
| `-term` | Exclude logs matching term | `-debug` |
| `term1 term2` | Match logs containing ALL terms (AND) | `error timeout` |

- **Case-insensitive**: All word and phrase matches are case-insensitive
- **AND logic**: Multiple terms must all match for a log line to be included
- **Negation**: Prefix any term with `-` to exclude matching lines
- **Quotes**: Use double (`"`) or single (`'`) quotes for phrases with spaces
- **Regex**: Enclose patterns in forward slashes (`/pattern/`) for regex matching

<CliCommand context="client">
```miren
# Filter for errors
miren logs -g error

# Exclude debug lines
miren logs -g "-debug"

# Match a phrase
miren logs -g '"connection refused"'

# Combine filters (must match both)
miren logs -g "error timeout"
```
</CliCommand>

## Time ranges

By default, logs show the last 100 lines. Use `--last` to specify a time range:

<CliCommand context="client">
```miren
# Last 5 minutes
miren logs --last 5m

# Last hour
miren logs --last 1h

# Last 24 hours
miren logs --last 24h
```
</CliCommand>

## Following logs

Use `--follow` (or `-f`) to stream logs in real-time:

<CliCommand context="client">
```miren
# Follow all logs
miren logs -f

# Follow logs for a specific app
miren logs app -a myapp -f
```
</CliCommand>

## Output format

Log entries are displayed with the following format:

```
S 2024-01-15 10:30:45: [source] Log message here
```

| Prefix | Meaning |
|--------|---------|
| `S` | stdout |
| `E` | stderr |
| `ERR` | error |
| `U` | user out-of-band |

Each entry includes a timestamp and an optional source identifier (such as a truncated sandbox ID) to help you trace which instance produced the log line.
