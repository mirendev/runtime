---
title: Crystal on Miren
description: Deploy Crystal apps on Miren with a Dockerfile.miren that compiles a single static binary.
keywords: [crystal, kemal, lucky, shards, static binary, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Crystal on Miren

Crystal isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles
your app to a single static binary and runs it on a minimal image. This pattern works
for the standard-library HTTP server as well as frameworks like Kemal and Lucky.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Crystal app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms your
server binds `0.0.0.0:$PORT`, wires up environment variables, and deploys — using this
page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Crystal yet, so add a `Dockerfile.miren` to your project
root. Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so your server must read `PORT` and
listen on `0.0.0.0`. With the standard library:

```crystal
require "http/server"

port = (ENV["PORT"]? || "8080").to_i

server = HTTP::Server.new do |context|
  context.response.content_type = "text/plain"
  context.response.print "Hello from Crystal on Miren!\n"
end

server.bind_tcp "0.0.0.0", port
server.listen
```

Kemal reads `PORT` differently — pass it to `Kemal.run(port)` or set `Kemal.config.host_binding = "0.0.0.0"`.

## The Dockerfile

Create `Dockerfile.miren` in your project root. The build compiles a **static** binary
(`--static`), so the runtime image needs no Crystal toolchain. Replace `crystal_bench`
with the target name from your `shard.yml`:

```dockerfile
# ----- Build stage -----
FROM crystallang/crystal:1.14.0-alpine AS builder

WORKDIR /app
COPY . .
# Build the named target; the binary lands at bin/<target>, copied below.
RUN shards build crystal_bench --release --static --no-debug

# ----- Runtime stage -----
FROM alpine:3.20

RUN apk add --no-cache gc pcre2 libgcc

COPY --from=builder /app/bin/crystal_bench /usr/local/bin/app

EXPOSE 8080
CMD ["app"]
```

The Alpine-based Crystal image is what makes `--static` work (it links against musl).
`shards build` reads `shard.yml`; a minimal one looks like:

```yaml
name: crystal_bench
version: 0.1.0

targets:
  crystal_bench:
    main: src/app.cr

crystal: ">= 1.0.0"
```

### .dockerignore

```text
.git
bin
lib
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: /usr/local/bin/app
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "crystal-bench"
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
output and logs). Read them in Crystal with `ENV["KEY"]`:

<CliCommand context="client">
```miren
miren env set -s DATABASE_URL
miren env set -s SECRET_KEY
```
</CliCommand>

You can also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "DATABASE_URL"
value = ""
required = true
sensitive = true
description = "Postgres connection string"
```

Need a managed Postgres database? Add a [`miren-postgresql` addon](/addons) and Miren
injects `DATABASE_URL` for you. See
[App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren` (static binary)
- **Build:** `shards build --release --static --no-debug` on `crystallang/crystal:*-alpine`
- **Runtime:** copy `bin/<target>` to a minimal Alpine image
- **Service is required:** define a `Procfile` (`web: /usr/local/bin/app`) — the image `CMD` is not used
- **Port:** read `ENV["PORT"]`; bind `0.0.0.0`
- **Env vars:** `miren env set -e/-s`, or `[[env]]` in `app.toml`; read with `ENV["KEY"]`
- **Database:** optional `[addons.miren-postgresql]` injects `DATABASE_URL`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Addons](/addons) — managed Postgres and other backing services
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
