package commands

const adminDescription = `The admin interface allows you to execute custom administrative functions in your running application—useful for user management, cache clearing, database operations, and other maintenance tasks.

## Listing Methods

Use the ` + "`" + `--list` + "`" + ` flag to discover what admin methods your application exposes:

` + "```" + `bash
$ miren app admin run --list

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

Usage: miren app admin run -a go-admin <method> [key=value ...]
` + "```" + `

## Parameter Validation

By default, ` + "`" + `miren app admin run` + "`" + ` validates method names and parameters against the application's introspection data before making the call. This catches typos and missing required parameters early.

To skip validation (e.g., if your app doesn't support introspection), use ` + "`" + `--no-validate` + "`" + `:

` + "```" + `bash
miren app admin run --no-validate some-method
` + "```" + `

## Output Formats

The admin command automatically chooses the output format based on context:

- **TTY (terminal)**: Uses a human-friendly pretty format by default
- **Non-TTY (pipes, scripts)**: Uses highlighted JSON by default

You can override this behavior:

` + "```" + `bash
# Force JSON output for scripting
miren app admin run --json get-stats | jq '.total_users'

# Force pretty output even when piping
miren app admin run --pretty get-user user_id=123
` + "```" + `

## Error Handling

If the admin call fails, the command exits with a non-zero status and displays the error:

` + "```" + `bash
$ miren app admin run get-user user_id=nonexistent
admin call failed (code -32001): user not found
` + "```" + `

Error codes follow JSON-RPC conventions:
- ` + "`" + `-32700` + "`" + `: Parse error
- ` + "`" + `-32600` + "`" + `: Invalid request
- ` + "`" + `-32601` + "`" + `: Method not found
- ` + "`" + `-32602` + "`" + `: Invalid params
- ` + "`" + `-32603` + "`" + `: Internal error
- Custom codes (negative numbers): Application-specific errors`
