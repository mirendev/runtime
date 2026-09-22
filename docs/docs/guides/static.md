---
title: Static sites & SPAs on Miren
description: Deploy static sites and single-page apps on Miren using direct file serving or Caddy.
keywords: [static, spa, single page app, vite, react, vue, astro, caddy, nginx, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Static sites & SPAs on Miren

Miren can export files from an app image into a dedicated artifact at deploy
time and serve them directly from HTTP ingress, without mounting the image or
running a sandbox. Set `static_dir` to the absolute directory containing the
built site. If you need SPA fallback or custom rewrites, run a web server such
as Caddy instead.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Vite app on Miren" after installing the
[Miren agent skills](../agent-skills.md). It sets `static_dir`, adds a build or
`Dockerfile.miren` when needed, and deploys — using this page as its reference.
:::

## Does this site need a Dockerfile?

Not when the repository already contains the files to serve. Miren maps the
uploaded source tree to `/app`, so `static_dir = "/app/site"` serves the local
`site/` directory directly without building an image.

A generated site whose tool is not natively detected needs a `Dockerfile.miren`.
Miren builds it before exporting `static_dir` — see
[Using Dockerfile.miren](./index.md#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Serve files directly

For a plain static site, put the files in a directory such as `site/`:

```text
site/
├── index.html
├── styles.css
└── images/
```

Then point ingress at its path under `/app`:

```toml title=".miren/app.toml"
name = "static-site"
static_dir = "/app/site"
```

For a generated site, use a build stage and copy only its output into the final image:

```dockerfile title="Dockerfile.miren"
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM scratch
COPY --from=builder /app/dist /app/site
```

Ingress serves regular files and directory `index.html` files. Missing paths return
404. No service or sandbox is created for this configuration. If the app also declares
a `web` service, missing files fall through to that service instead.

## Use Caddy for SPA fallback

Caddy reads the injected `$PORT` and serves your build directory, falling back to
`index.html` so client-side routing works. Create a `Caddyfile`:

```caddyfile
:{$PORT:8080} {
	root * /site
	try_files {path} /index.html
	file_server
}
```

`{$PORT:8080}` uses the `PORT` environment variable Miren injects, defaulting to 8080
for local runs.

## The Dockerfile

For a plain static site, copy your files and the Caddyfile into the Caddy image:

```dockerfile
FROM caddy:2-alpine

COPY site /site
COPY Caddyfile /etc/caddy/Caddyfile

EXPOSE 8080
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
```

For a built SPA (Vite, Astro, Create React App), add a build stage and copy the output
into `/site`:

```dockerfile
# ----- Build stage -----
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# ----- Serve stage -----
FROM caddy:2-alpine
COPY --from=builder /app/dist /site
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 8080
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
```

Point the `COPY --from=builder` at your framework's output directory (`dist` for Vite,
`build` for Create React App, `dist` for Astro).

### .dockerignore

```text
.git
node_modules
dist
```

## Deploy

The Dockerfile's `CMD` starts the app, so you don't need a `Procfile` or service command.
The Caddy base image exposes several HTTP, HTTPS, and admin ports; select this app's HTTP
port explicitly.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "static-bench"

[services.web]
port = 8080
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Static assets are built at image-build time, so runtime environment variables don't
reach the browser. Bake build-time configuration in during `npm run build` (e.g. Vite's
`VITE_*` variables), or fetch runtime config from an API your SPA calls.

If you do need a value at build time, pass it as a Docker build arg and reference it in
your build step. See [App Configuration](../app-configuration.md) for how Miren handles
configuration.

## Agent quick reference

- **Detection:** `static_dir` is sufficient for source that is already static; generated sites need a detected stack or `Dockerfile.miren`
- **Direct serve:** set `static_dir` to export and serve built files without a sandbox
- **SPA fallback:** use `caddy:2-alpine` with a `Caddyfile` using `:{$PORT:8080}` and `try_files {path} /index.html`
- **SPA build:** add a `node:20-alpine` build stage, copy the output dir into `/site`
- **Startup:** direct serving needs no `CMD`, `Procfile`, or service command
- **Port:** Caddy binds `:{$PORT}` from the environment
- **Runtime env:** not visible to the browser; use build-time `VITE_*` vars or a runtime config API

## Next steps

- [Using Dockerfile.miren](./index.md#using-dockerfilemiren) — how custom builds work
- [App Configuration](../app-configuration.md) — customize `.miren/app.toml`
- [Deployment](../deployment.md) — how deploys build and activate
- [JavaScript on Miren](./javascript.md) — if you also run a Node/Bun backend
