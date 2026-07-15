---
title: "miren logs"
sidebar_label: "logs"
description: "View logs (defaults to app logs)"
---

# miren logs

View logs (defaults to app logs)

## Subcommands

`miren logs` has subcommands for different log sources:

```bash
miren logs app       # Application logs (default)
miren logs sandbox   # Sandbox logs
miren logs build     # Build logs
miren logs system    # System/server logs
```

Running `miren logs` without a subcommand shows app logs (backward compatible).

## Time Range

By default, logs show the last 100 lines. Use `--since` and `--until` to bound the window. Each accepts an RFC3339 timestamp, a friendlier `2006-01-02 15:04` style time (interpreted in your local timezone), or a duration that's read as "ago":

```bash
# Last 5 minutes (a duration is read as "ago")
miren logs --since 5m

# A bounded historical window, e.g. chasing an incident
miren logs --since "2026-06-25 14:00" --until "2026-06-25 14:30"

# From an absolute start up to now
miren logs --since 2026-06-25T14:00:00Z

# Everything up to a point in the past (start of retention through --until)
miren logs --until "2026-06-25 14:30"
```

`--since` and `--until` compose with `--grep` and `--service`. `--until` can't be combined with `--follow` (a live tail has no end). The older `--last` flag still works and is equivalent to `--since` with a duration:

```bash
# These two are equivalent
miren logs --last 1h
miren logs --since 1h
```

## Following Logs

Use `--follow` (or `-f`) to stream logs in real-time:

```bash
# Follow logs as they arrive
miren logs -f

# Follow logs for a specific app
miren logs app -a myapp -f
```

## Filtering by Service

Use `--service` to filter logs by service name (app logs only):

```bash
# Show only logs from the web service
miren logs app --service web

# Show worker service logs containing "error"
miren logs app --service worker -g error
```

## Filtering Logs

Use the `--grep` (or `-g`) flag to filter log output. The filter supports multiple syntax options for flexible searching.

### Filter Syntax

| Syntax | Description | Example |
|--------|-------------|---------|
| `word` | Match logs containing "word" (case-insensitive) | `error` |
| `"phrase"` | Match logs containing exact phrase | `"connection failed"` |
| `'phrase'` | Match logs containing exact phrase (alternate) | `'connection failed'` |
| `/regex/` | Match logs matching regex pattern | `/err(or)?/` |
| `-term` | Exclude logs matching term | `-debug` |
| `term1 term2` | Match logs containing ALL terms (AND) | `error timeout` |

### Filter Details

- **Case-insensitive**: All word and phrase matches are case-insensitive
- **AND logic**: Multiple terms must all match for a log line to be included
- **Negation**: Prefix any term with `-` to exclude matching lines
- **Quotes**: Use double (`"`) or single (`'`) quotes for phrases with spaces
- **Regex**: Enclose patterns in forward slashes (`/pattern/`) for regex matching

## Log Output Format

Log entries are displayed with the following format:

```
S 2024-01-15 10:30:45: [source] Log message here
```

- **Stream prefix**: `S` (stdout), `E` (stderr), `ERR` (error), `U` (user-oob)
- **Timestamp**: Date and time when the log was generated
- **Source**: Optional source identifier (sandbox ID, truncated if long)
- **Message**: The actual log content

## Usage

```bash
miren logs [flags]
```

## Flags

- `--follow, -f` — Follow log output (live tail)
- `--format` — Output format (text, json) (default: `text`)
- `--grep, -g` — Filter logs (e.g., 'error', '"exact phrase"', 'error -debug', '/regex/')
- `--json` — Shorthand for --format json
- `--last, -l` — Show logs from the last duration
- `--service` — Filter logs by service name (e.g., 'web', 'worker')
- `--since` — Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)
- `--until` — Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**View logs for the current app:**

```bash
miren logs
```

**Follow logs in real time:**

```bash
miren logs -f
```

**Show logs from the last 5 minutes, filtered for errors:**

```bash
miren logs --last 5m -g error
```

## Subcommands

- [`miren logs app`](/command/logs-app) — View application logs
- [`miren logs build`](/command/logs-build) — View build logs
- [`miren logs sandbox`](/command/logs-sandbox) — View sandbox logs
- [`miren logs system`](/command/logs-system) — View system logs
