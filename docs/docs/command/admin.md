---
title: "miren admin"
sidebar_label: "admin"
description: "Call an admin method on an application"
---

# miren admin

Call an admin method on an application

The admin interface allows you to execute custom administrative functions in your running application—useful for user management, cache clearing, database operations, and other maintenance tasks.

## Listing Methods

Use the `--list` flag to discover what admin methods your application exposes:

```bash
$ miren admin --list

Admin methods for go-admin

  clear-cache
  │ Clear the application cache

  delete-user
  │ Delete a user by ID
  └ user_id string

  get-stats
  │ Get application statistics

  get-user
  │ Get a specific user by ID
  └ user_id string

  list-users
  │ List all users in the system
  ├ limit number
  └ offset number

Usage: miren admin -a go-admin <method> [key=value ...]
```

## Parameter Validation

By default, `miren admin` validates method names and parameters against the application's introspection data before making the call. This catches typos and missing required parameters early.

To skip validation (e.g., if your app doesn't support introspection), use `--no-validate`:

```bash
miren admin --no-validate some-method
```

## Output Formats

The admin command automatically chooses the output format based on context:

- **TTY (terminal)**: Uses a human-friendly pretty format by default
- **Non-TTY (pipes, scripts)**: Uses highlighted JSON by default

You can override this behavior:

```bash
# Force JSON output for scripting
miren admin --json get-stats | jq '.total_users'

# Force pretty output even when piping
miren admin --pretty get-user user_id=123
```

## Error Handling

If the admin call fails, the command exits with a non-zero status and displays the error:

```bash
$ miren admin get-user user_id=nonexistent
admin call failed (code -32001): user not found
```

Error codes follow JSON-RPC conventions:
- `-32700`: Parse error
- `-32600`: Invalid request
- `-32601`: Method not found
- `-32602`: Invalid params
- `-32603`: Internal error
- Custom codes (negative numbers): Application-specific errors

## Usage

```bash
miren admin <method> [args...] [flags]
```

## Arguments

- `method` — Admin method to call

## Flags

- `--func-help` — Show help for a specific admin method
- `--json, -j` — Output as highlighted JSON (default for non-TTY)
- `--list, -l` — List available admin methods
- `--no-validate` — Skip method/parameter validation
- `--params-file, -f` — Read params as JSON from file (use - for stdin)
- `--pretty, -p` — Render output in a human-friendly format (default for TTY)

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

**List available admin methods:**

```bash
miren admin --list -a myapp
```

**Call an admin method:**

```bash
miren admin health -a myapp
```

**Call a method with JSON output:**

```bash
miren admin stats -a myapp --json
```

**Call a method with params from a file:**

```bash
miren admin migrate -a myapp -f params.json
```
