---
title: Lua on Miren
description: Deploy Lua apps on Miren with a Dockerfile.miren using lua-http installed via LuaRocks.
keywords: [lua, lua-http, luarocks, openresty, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Lua on Miren

Lua isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[lua-http](https://github.com/daurnimator/lua-http) — a proper Lua HTTP server —
installed with LuaRocks. (For a heavier stack, OpenResty (nginx + Lua) works too.)

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Lua app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds the server to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Lua, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`. With
lua-http:

```lua
local server = require "http.server"
local headers = require "http.headers"

local port = tonumber(os.getenv("PORT") or "8080")

local function on_stream(_, stream)
  stream:get_headers()
  local res = headers.new()
  res:append(":status", "200")
  res:append("content-type", "text/plain")
  stream:write_headers(res, false)
  stream:write_chunk("Hello from Lua on Miren!\n", true)
end

local s = assert(server.listen {
  host = "0.0.0.0",
  port = port,
  onstream = on_stream,
})
assert(s:listen())
print("listening on 0.0.0.0:" .. port)
assert(s:loop())
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. LuaRocks builds lua-http and its C
dependencies (cqueues, luaossl), so the image needs a compiler and OpenSSL headers:

```dockerfile
FROM debian:12-slim
RUN apt-get update -y \
    && apt-get install -y lua5.4 liblua5.4-dev luarocks gcc make m4 libssl-dev git \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
RUN luarocks --lua-version=5.4 install http
COPY . /app
EXPOSE 8080
```

:::note[Pin the Lua version for LuaRocks]
Debian's `luarocks` can target several Lua versions. Pass `--lua-version=5.4` (matching
the `lua5.4`/`liblua5.4-dev` packages) so the rock and its C parts build against the
same interpreter you run with.
:::

### .dockerignore

```text
.git
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: lua5.4 /app/app.lua
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "lua-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

:::note[Deploying without a service fails]
If no service is defined, the build succeeds but the deploy stops with
`no services defined: please define at least one service in a Procfile or
.miren/app.toml`.
:::

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `os.getenv("KEY")`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s API_TOKEN
```
</CliCommand>

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Library:** lua-http via `luarocks --lua-version=5.4 install http` (needs `gcc make m4 libssl-dev`)
- **Service is required:** define a `Procfile` (`web: lua5.4 /app/app.lua`) — the image `CMD` is not used
- **Port:** `os.getenv("PORT")`; `server.listen { host = "0.0.0.0", port = port }`
- **Env vars:** `miren env set -e/-s`; read with `os.getenv`
- **Heavier option:** OpenResty (nginx + Lua) for a production Lua web stack

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
