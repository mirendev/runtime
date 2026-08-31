---
title: Static sites & SPAs on Miren
description: Deploy static sites and single-page apps on Miren with a Dockerfile.miren that serves built assets with Caddy.
keywords: [static, spa, single page app, vite, react, vue, astro, caddy, nginx, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Static sites & SPAs on Miren

Miren runs services, not a static file host — but a static site or single-page app is
just a tiny web server. You deploy one with a `Dockerfile.miren` that builds your assets
and serves them with [Caddy](https://caddyserver.com), which handles SPA fallback and
reads the injected `$PORT`.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Vite app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the build step and `Dockerfile.miren`,
configures the SPA fallback, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Add a `Dockerfile.miren` to your project root. Miren builds from it instead of
guessing the stack — see [Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## The Caddyfile

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
your build step. See [App Configuration](/app-configuration) for how Miren handles
configuration.

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Serve:** `caddy:2-alpine` with a `Caddyfile` using `:{$PORT:8080}` and `try_files {path} /index.html`
- **SPA build:** add a `node:20-alpine` build stage, copy the output dir into `/site`
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** Caddy binds `:{$PORT}` from the environment
- **Runtime env:** not visible to the browser; use build-time `VITE_*` vars or a runtime config API

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
- [JavaScript on Miren](/guides/javascript) — if you also run a Node/Bun backend
