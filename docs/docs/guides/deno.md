---
title: Deno on Miren
description: Deploy Deno apps on Miren with a Dockerfile.miren using the official Deno image.
keywords: [deno, typescript, javascript, fresh, oak, deno serve, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Deno on Miren

Miren auto-detects Node and Bun, but not Deno — so you deploy Deno with a
`Dockerfile.miren` built on the official Deno image. It runs TypeScript directly, so
there's no separate build step for most apps.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Deno app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms your
server binds `0.0.0.0:$PORT`, sets the runtime permissions, and deploys — using this
page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren's JavaScript detection covers Node and Bun (see
[JavaScript on Miren](/guides/javascript)); Deno needs a `Dockerfile.miren`. Miren builds
from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so your server must read `PORT` and
listen on `0.0.0.0`. With the built-in `Deno.serve`:

```ts
const port = Number(Deno.env.get("PORT") ?? "8080");

Deno.serve({ port, hostname: "0.0.0.0" }, () =>
  new Response("Hello from Deno on Miren!\n", {
    headers: { "content-type": "text/plain" },
  }));
```

Frameworks like Oak or Fresh accept the same `port`/`hostname` — pass the injected
`PORT` and bind `0.0.0.0`.

## The Dockerfile

Create `Dockerfile.miren` in your project root:

```dockerfile
FROM denoland/deno:2.1.4

WORKDIR /app
COPY . .
RUN deno cache main.ts

EXPOSE 8080
CMD ["run", "--allow-net", "--allow-env", "main.ts"]
```

`deno cache` pre-fetches and compiles your dependencies into the image so startup is
fast. Deno runs with no permissions by default, so grant exactly what your app needs —
`--allow-net` to listen, `--allow-env` to read `PORT`, and any others (`--allow-read`,
`--allow-write`) your code requires.

### .dockerignore

```text
.git
```

## Set up the app

Add a `Procfile` with the full
`deno run` command (including permissions):

```procfile
web: deno run --allow-net --allow-env main.ts
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "deno-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `Deno.env.get("KEY")` (needs `--allow-env`):

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s DATABASE_URL
miren env set -s API_TOKEN
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

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren` (Node/Bun are detected, Deno is not)
- **Base image:** `denoland/deno:<version>`; `deno cache main.ts` warms deps
- **Service is required:** define a `Procfile` (`web: deno run --allow-net --allow-env main.ts`) — the image `CMD` is not used
- **Permissions:** grant `--allow-net` + `--allow-env` at minimum; add others as needed
- **Port:** read `Deno.env.get("PORT")`; bind `0.0.0.0` via `Deno.serve`
- **Env vars:** `miren env set -e/-s`, or `[[env]]` in `app.toml`; read with `Deno.env.get`

## Next steps

- [JavaScript on Miren](/guides/javascript) — Node and Bun (auto-detected)
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
