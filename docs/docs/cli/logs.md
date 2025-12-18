---
sidebar_position: 4
---

# Logs Command

View logs from your Miren applications.

## miren logs

Get logs for an application or sandbox.

### Usage

```bash
miren logs [flags]
```

### Flags

- `--app, -a` - Application to get logs for (or use `$MIREN_APP` environment variable)
- `--dir, -d` - Directory to run from (default: ".")
- `--last, -l` - Show logs from the last duration (e.g., 5m, 1h)
- `--sandbox, -s` - Show logs for a specific sandbox ID
- `--follow, -f` - Follow log output (live tail)
- `--grep, -g` - Filter logs using the filter syntax
- `--service` - Filter logs by service name (e.g., web, worker)

### Examples

```bash
# View logs for app in current directory
miren logs

# View logs for a specific app
miren logs --app myapp

# View logs for a specific sandbox
miren logs --sandbox abc123

# Show logs from the last 5 minutes
miren logs --last 5m

# Show logs from the last hour
miren logs --last 1h

# Follow logs as they arrive
miren logs --follow

# Filter logs containing "error"
miren logs -g error

# Filter logs by service
miren logs --service web

# Combine service filter with text filter
miren logs --service worker -g error
```

## Time Range

By default, logs show entries from today. Use `--last` to specify a different time range:

```bash
# Show logs from the last 5 minutes
miren logs --last 5m

# Show logs from the last hour
miren logs --last 1h

# Show logs from the last 24 hours
miren logs --last 24h
```

## Following Logs

Use `--follow` (or `-f`) to stream logs in real-time:

```bash
# Follow logs as they arrive
miren logs --follow

# Follow logs for a specific app
miren logs --app myapp -f
```

## Filtering by Service

Use `--service` to filter logs by service name. This is useful for applications with multiple services (e.g., web, worker):

```bash
# Show only logs from the web service
miren logs --service web

# Show worker service logs containing "error"
miren logs --service worker -g error

# Follow web service logs
miren logs --service web -f
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

### Filter Examples

```bash
# Find logs containing "error"
miren logs -g error

# Find logs with exact phrase
miren logs -g '"connection failed"'

# Find logs containing "error" but not "debug"
miren logs -g 'error -debug'

# Use regex to match patterns
miren logs -g '/err(or)?/'

# Combine multiple terms (AND logic)
miren logs -g 'error timeout'

# Complex filter: contains "error" AND matches regex, excludes "debug"
miren logs -g 'error /warn(ing)?/ -debug'
```

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

## Next Steps

- [CLI Reference](/cli-reference) - See all available commands
- [App Commands](/cli/app) - Application management commands
- [Getting Started](/getting-started) - Deploy your first app
