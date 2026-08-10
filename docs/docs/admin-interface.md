---
title: Admin Interface
description: Expose custom administrative functions in your app that can be called from the CLI via JSON-RPC.
keywords: [admin, json-rpc, maintenance, management, cli]
---

import CliCommand from '@site/src/components/CliCommand';

# Admin Interface

The admin interface allows you to expose custom administrative functions in your application that can be called from the CLI or other tooling. This is useful for user management, cache clearing, database operations, and other maintenance tasks.

## Minimum working example

Handle JSON-RPC 2.0 at `/.well-known/miren/admin` in your web service, validating the injected `ADMIN_TOKEN`:

```typescript
Bun.serve({
  port: process.env.PORT ?? 3000,
  async fetch(req) {
    const url = new URL(req.url);
    if (req.method !== 'POST' || url.pathname !== '/.well-known/miren/admin') {
      return new Response('Not Found', { status: 404 });
    }
    if (req.headers.get('authorization') !== `Bearer ${process.env.ADMIN_TOKEN}`) {
      return new Response('Unauthorized', { status: 401 });
    }
    const { method, id } = await req.json();
    if (method === 'ping') return Response.json({ jsonrpc: '2.0', id, result: 'pong' });
    return Response.json({ jsonrpc: '2.0', id, error: { code: -32601, message: 'Method not found' } });
  },
});
```

Deploy, then call it from the CLI:

<CliCommand context="client">
```miren
miren admin -a myapp ping
```
</CliCommand>

The [complete examples](#complete-example-go) below add introspection, typed parameters, and constant-time token comparison.

## How It Works

Your application exposes admin methods via a JSON-RPC 2.0 endpoint at a well-known path. When you run `miren admin`, the CLI:

1. Looks up your app and retrieves the admin token
2. Sends a JSON-RPC request to your app's web service
3. Returns the response (or error) to you

```text
miren admin delete-user user_id=123 --app myapp
         |
         v
    JSON-RPC POST to /.well-known/miren/admin
    Authorization: Bearer <ADMIN_TOKEN>
    {"jsonrpc":"2.0","method":"delete-user","params":{"user_id":"123"},"id":1}
         |
         v
    Your app processes the request and returns a response
```

## Implementing the Admin Endpoint

### Endpoint Requirements

Your web service must expose:

| Requirement | Value |
|-------------|-------|
| Path | `/.well-known/miren/admin` |
| Method | POST |
| Content-Type | `application/json` |
| Protocol | JSON-RPC 2.0 |

### Security

Admin calls are authenticated using a bearer token that Miren generates for your app. Your app receives this token via the `ADMIN_TOKEN` environment variable and must validate it on every request.

#### The admin token

- **Format**: 32 random bytes, a random bearer token
- **Per-version**: a fresh token is generated for every new app version at build time, so each deploy rotates the token automatically.
- **Reserved env var**: `ADMIN_TOKEN` is injected by the runtime. It is appended after your own env vars so it cannot be overridden from `miren.toml`, the CLI, or build-time env.

**Validate the token on every request, for example in Go:**

```go
func authMiddleware(token string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if token != "" {
            auth := r.Header.Get("Authorization")
            if !strings.HasPrefix(auth, "Bearer ") {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            if subtle.ConstantTimeCompare(
                []byte(strings.TrimPrefix(auth, "Bearer ")),
                []byte(token),
            ) != 1 {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
        }
        next.ServeHTTP(w, r)
    })
}
```

#### Network reachability and `X-Miren-Access`

Miren's admin proxy talks to your app over its **internal** HTTP ingress path, never via the public route. Any public request to `/.well-known/miren/admin` is rejected with `404 Not Found` by the ingress before it can reach your app, so in practice your handler only ever sees requests carrying the `X-Miren-Access: internal` header.

For reference, the two values the runtime uses are:

- `X-Miren-Access: internal` — request originated from `miren admin` and was routed through Miren's internal admin path.
- `X-Miren-Access: public` — request arrived through the public ingress. Miren overwrites any client-supplied value here before forwarding, so the header cannot be spoofed by external callers.

The bearer token is the primary guard; checking for `X-Miren-Access: internal` is a defense-in-depth signal in case the well-known path is ever exposed through a custom route.

### Auditing

Every admin call is appended to the **app's log stream** as an out-of-band entry (`source=admin`, `method=<name>`) including the method name, params payload size, status (`ok` / `error=...`), and duration in milliseconds. You can review the audit trail with:

```bash
miren logs <app>
```

This is server-side bookkeeping — your handler doesn't need to log calls itself to get an audit record.

### JSON-RPC 2.0 Format

Requests follow the standard JSON-RPC 2.0 format:

```json
{
  "jsonrpc": "2.0",
  "method": "method-name",
  "params": {"key": "value"},
  "id": 1
}
```

`params` may be a JSON **object** (named arguments, recommended) or a JSON **array** (positional arguments). When the CLI describes a method that uses positional arguments via `$methods`, it labels the entries `arg0`, `arg1`, etc.

#### Call timeout

Each admin call has a fixed **30-second** timeout enforced by the runtime. Handlers should return promptly — if work takes longer, enqueue it on a background worker and return a job handle the caller can poll on a subsequent admin method.

#### HTTP status semantics

The runtime expects a `200 OK` response carrying a JSON-RPC envelope (success *or* error). A non-200 HTTP status is surfaced to the caller as a generic `admin endpoint returned status N` message with no structured detail. Prefer returning a JSON-RPC error object over a raw HTTP error code so the CLI can render a useful message and error code.

Successful responses:

```json
{
  "jsonrpc": "2.0",
  "result": {"any": "data"},
  "id": 1
}
```

Error responses:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32001,
    "message": "user not found",
    "data": {"user_id": "123"}
  },
  "id": 1
}
```

### Standard Error Codes

| Code | Meaning |
|------|---------|
| -32700 | Parse error (invalid JSON) |
| -32600 | Invalid request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32603 | Internal error |
| < 0 | Application-specific errors |

## Method Introspection

When you run `miren admin --list`, Miren sends a JSON-RPC request with the reserved method name `$methods` to your admin endpoint. If your app handles this method, it should return an array of objects describing the available admin methods. This is optional — if your app doesn't handle `$methods`, the `--list` command will report an error, but regular method calls still work.

:::note[Reserved method names]
Names beginning with `$` are reserved for the runtime. The CLI currently uses `$methods` for discovery and filters both `$methods` and `$type` out of `--list` output, so don't expose business logic under those names.
:::

The `$methods` request has no params:

```json
{
  "jsonrpc": "2.0",
  "method": "$methods",
  "id": 1
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "result": [
    {
      "name": "list-users",
      "description": "List all users in the system",
      "category": "users",
      "params": {"limit": "number", "offset": "number"}
    },
    {
      "name": "get-user",
      "description": "Get a specific user by ID",
      "category": "users",
      "params": {"user_id": "string"}
    },
    {
      "name": "clear-cache",
      "description": "Clear the application cache",
      "category": "maintenance"
    }
  ],
  "id": 1
}
```

Method metadata fields:

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Method name |
| `description` | No | Human-readable description |
| `category` | No | Grouping for display (e.g., "users", "maintenance") |
| `params` | No | Parameter definitions as `{"name": "type"}` |

## Complete Example (Go)

Here's a complete example using the `jsonrpc3` library:

```go
package main

import (
    "context"
    "crypto/subtle"
    "log"
    "net/http"
    "os"
    "strings"

    "miren.dev/jsonrpc3/go/jsonrpc3"
)

func main() {
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }

    adminToken := os.Getenv("ADMIN_TOKEN")
    if adminToken == "" {
        log.Println("WARNING: ADMIN_TOKEN not set")
    }

    // Create the admin handler with method definitions
    adminMethods := jsonrpc3.NewMethodMap()

    // Register admin methods with introspection metadata
    adminMethods.Register("list-users", listUsers,
        jsonrpc3.WithDescription("List all users in the system"),
        jsonrpc3.WithParams(map[string]string{
            "limit":  "number",
            "offset": "number",
        }),
        jsonrpc3.WithCategory("users"),
    )

    adminMethods.Register("get-user", getUser,
        jsonrpc3.WithDescription("Get a specific user by ID"),
        jsonrpc3.WithParams(map[string]string{
            "user_id": "string",
        }),
        jsonrpc3.WithCategory("users"),
    )

    adminMethods.Register("clear-cache", clearCache,
        jsonrpc3.WithDescription("Clear the application cache"),
        jsonrpc3.WithCategory("maintenance"),
    )

    // Create the HTTP handler for JSON-RPC
    rpcHandler := jsonrpc3.NewHTTPHandler(adminMethods)

    // Wrap with auth middleware
    authHandler := authMiddleware(adminToken, rpcHandler)

    // Mount admin endpoint at well-known path
    http.Handle("/.well-known/miren/admin", authHandler)

    log.Printf("Starting server on port %s", port)
    log.Fatal(http.ListenAndServe(":"+port, nil))
}

func authMiddleware(token string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if token != "" {
            auth := r.Header.Get("Authorization")
            if !strings.HasPrefix(auth, "Bearer ") {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            if subtle.ConstantTimeCompare(
                []byte(strings.TrimPrefix(auth, "Bearer ")),
                []byte(token),
            ) != 1 {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
        }
        next.ServeHTTP(w, r)
    })
}

// Sample data
var users = map[string]map[string]any{
    "user-1": {"id": "user-1", "name": "Alice", "email": "alice@example.com"},
    "user-2": {"id": "user-2", "name": "Bob", "email": "bob@example.com"},
}

type listUsersParams struct {
    Limit  int `json:"limit"`
    Offset int `json:"offset"`
}

func listUsers(ctx context.Context, params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
    p := listUsersParams{Limit: 10, Offset: 0}
    if params != nil {
        _ = params.Decode(&p)
    }

    var result []map[string]any
    for _, user := range users {
        result = append(result, user)
    }

    return map[string]any{
        "users": result,
        "total": len(users),
    }, nil
}

type userIDParams struct {
    UserID string `json:"user_id"`
}

func getUser(ctx context.Context, params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
    var p userIDParams
    if params == nil {
        return nil, jsonrpc3.NewInvalidParamsError("user_id is required")
    }
    if err := params.Decode(&p); err != nil {
        return nil, jsonrpc3.NewInvalidParamsError("invalid params")
    }
    if p.UserID == "" {
        return nil, jsonrpc3.NewInvalidParamsError("user_id is required")
    }

    user, ok := users[p.UserID]
    if !ok {
        return nil, jsonrpc3.NewError(-32001, "user not found", nil)
    }

    return user, nil
}

func clearCache(ctx context.Context, params jsonrpc3.Params, caller jsonrpc3.Caller) (any, error) {
    // Your cache clearing logic here
    return map[string]any{
        "cleared": true,
    }, nil
}
```

## Other Languages

The admin interface is language-agnostic. Any language that can:

1. Handle HTTP POST requests
2. Parse and generate JSON
3. Implement the JSON-RPC 2.0 protocol

can expose admin methods. The key requirements are:

- Listen on `/.well-known/miren/admin`
- Validate the `Authorization: Bearer <token>` header against `ADMIN_TOKEN`
- Handle JSON-RPC requests and return proper responses
- Optionally implement `$methods` for introspection

See [More Implementation Examples](#more-implementation-examples) at the bottom of this page for ready-to-use Python, Node.js, and Bun snippets.

## Calling Admin Methods

Once your app exposes the admin interface, use the CLI to call methods. All commands target the active version of the app named with `--app` / `-a` (or inferred from the current directory).

### Discovery and help

<CliCommand context="client">

```miren
# List every method the app advertises via $methods
miren admin --list -a myapp

# Show parameter signature for one method (--func-help or -h <method>)
miren admin -a myapp get-user -h
miren admin -a myapp --func-help get-user
```

</CliCommand>

### Passing parameters

You can pass parameters three ways, and mix them freely in one call. The CLI rejects the call if the same key shows up via more than one channel.

<CliCommand context="client">

```miren
# 1. Bare key=value pairs
miren admin -a myapp get-user user_id=user-1

# 2. Long flags: --key=value or --key value
miren admin -a myapp list-users --limit 50 --offset 0

# 3. A JSON params object from a file (use - for stdin)
miren admin -a myapp update-config -f settings.json
cat settings.json | miren admin -a myapp update-config -f -

# Mixed: file supplies defaults, flag overrides one field
miren admin -a myapp update-config -f settings.json --debug=true
```

</CliCommand>

#### Type-aware parsing

When the app advertises a parameter type via `$methods`, the CLI coerces the supplied string into that type before sending the JSON-RPC request:

| Declared type | Accepted CLI input |
|---------------|--------------------|
| `string` | any value, passed through |
| `number`, `integer`, `int`, `float` | numeric literal (`42`, `3.14`, `-5`) |
| `boolean`, `bool` | `true` / `false` / `1` / `0` / `yes` / `no` |
| `object` | JSON object literal (`'{"k":"v"}'`) |
| `array` | JSON array literal (`'[1,2,3]'`) |

If the app does not advertise types (or you pass `--no-validate`), the CLI tries to parse each value as JSON and falls back to a string.

#### Kebab-case flag names

If your method declares a snake_case parameter like `user_id`, you can write the equivalent kebab-case flag (`--user-id`) on the CLI and it will be normalized automatically. Keys that genuinely contain hyphens are left alone.

### Output format

The CLI chooses an output format based on context:

- **TTY**: human-friendly pretty rendering (tables for uniform arrays, key/value lists otherwise).
- **Non-TTY** (pipes, scripts): syntax-highlighted JSON.

Override with `--json` to force JSON or `--pretty` to force the rendered form.

### Skipping validation

If your app does not implement `$methods`, the CLI silently skips validation. To suppress validation explicitly — for example to call an undeclared diagnostic method — pass `--no-validate`:

```bash
miren admin -a myapp --no-validate debug-internal
```

See [Admin Commands](/command/admin) for the full CLI flag reference.

## More Implementation Examples

### Python Example

Using Flask:

```python
from flask import Flask, request, jsonify
import hmac
import os

app = Flask(__name__)
ADMIN_TOKEN = os.environ.get('ADMIN_TOKEN', '')

@app.route('/.well-known/miren/admin', methods=['POST'])
def admin_endpoint():
    # Validate token
    auth = request.headers.get('Authorization', '')
    if ADMIN_TOKEN and not hmac.compare_digest(auth, f'Bearer {ADMIN_TOKEN}'):
        return 'Unauthorized', 401

    data = request.json
    method = data.get('method')
    params = data.get('params', {})
    req_id = data.get('id')

    # Handle introspection
    if method == '$methods':
        return jsonify({
            'jsonrpc': '2.0',
            'result': [
                {'name': 'get-stats', 'description': 'Get app statistics'},
            ],
            'id': req_id
        })

    # Handle your methods
    if method == 'get-stats':
        return jsonify({
            'jsonrpc': '2.0',
            'result': {'users': 42, 'requests': 1000},
            'id': req_id
        })

    return jsonify({
        'jsonrpc': '2.0',
        'error': {'code': -32601, 'message': 'Method not found'},
        'id': req_id
    })
```

### Node.js Example

Using Express:

```javascript
const express = require('express');
const crypto = require('crypto');

const app = express();
app.use(express.json());

const ADMIN_TOKEN = process.env.ADMIN_TOKEN || '';

function tokenMatches(supplied, expected) {
  const a = Buffer.from(supplied);
  const b = Buffer.from(expected);
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}

app.post('/.well-known/miren/admin', (req, res) => {
  const auth = req.get('authorization') || '';
  if (ADMIN_TOKEN) {
    if (!auth.startsWith('Bearer ') || !tokenMatches(auth.slice(7), ADMIN_TOKEN)) {
      return res.status(401).send('Unauthorized');
    }
  }

  const { method, params = {}, id } = req.body;

  if (method === '$methods') {
    return res.json({
      jsonrpc: '2.0',
      id,
      result: [
        {
          name: 'get-stats',
          description: 'Get app statistics',
          category: 'maintenance',
        },
        {
          name: 'get-user',
          description: 'Get a specific user by ID',
          category: 'users',
          params: { user_id: 'string' },
        },
      ],
    });
  }

  if (method === 'get-stats') {
    return res.json({
      jsonrpc: '2.0',
      id,
      result: { users: 42, requests: 1000 },
    });
  }

  if (method === 'get-user') {
    if (!params.user_id) {
      return res.json({
        jsonrpc: '2.0',
        id,
        error: { code: -32602, message: 'user_id is required' },
      });
    }
    return res.json({
      jsonrpc: '2.0',
      id,
      result: { id: params.user_id, name: 'Alice' },
    });
  }

  return res.json({
    jsonrpc: '2.0',
    id,
    error: { code: -32601, message: 'Method not found' },
  });
});

const port = process.env.PORT || 8080;
app.listen(port, () => console.log(`listening on :${port}`));
```

### Bun Example

Bun's built-in HTTP server needs no dependencies:

```typescript
import { timingSafeEqual } from 'node:crypto';

const ADMIN_TOKEN = process.env.ADMIN_TOKEN ?? '';

function tokenMatches(supplied: string, expected: string): boolean {
  const a = Buffer.from(supplied);
  const b = Buffer.from(expected);
  if (a.length !== b.length) return false;
  return timingSafeEqual(a, b);
}

type RpcRequest = {
  jsonrpc: '2.0';
  method: string;
  params?: Record<string, unknown> | unknown[];
  id: number | string | null;
};

function rpc(id: RpcRequest['id'], body: object): Response {
  return Response.json({ jsonrpc: '2.0', id, ...body });
}

Bun.serve({
  port: Number(process.env.PORT ?? 8080),
  async fetch(req) {
    const url = new URL(req.url);
    if (req.method !== 'POST' || url.pathname !== '/.well-known/miren/admin') {
      return new Response('Not Found', { status: 404 });
    }

    if (ADMIN_TOKEN) {
      const auth = req.headers.get('authorization') ?? '';
      if (!auth.startsWith('Bearer ') || !tokenMatches(auth.slice(7), ADMIN_TOKEN)) {
        return new Response('Unauthorized', { status: 401 });
      }
    }

    const { method, params = {}, id } = (await req.json()) as RpcRequest;
    const p = params as Record<string, unknown>;

    switch (method) {
      case '$methods':
        return rpc(id, {
          result: [
            { name: 'get-stats', description: 'Get app statistics', category: 'maintenance' },
            {
              name: 'get-user',
              description: 'Get a specific user by ID',
              category: 'users',
              params: { user_id: 'string' },
            },
          ],
        });

      case 'get-stats':
        return rpc(id, { result: { users: 42, requests: 1000 } });

      case 'get-user':
        if (!p.user_id) {
          return rpc(id, { error: { code: -32602, message: 'user_id is required' } });
        }
        return rpc(id, { result: { id: p.user_id, name: 'Alice' } });

      default:
        return rpc(id, { error: { code: -32601, message: 'Method not found' } });
    }
  },
});
```

## Next Steps

- [Admin Commands](/command/admin) — CLI reference for `miren admin`
- [Services](/services) — Configure your app's web service
- [Getting Started](/getting-started) — Deploy your first app
