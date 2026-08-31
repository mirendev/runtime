---
title: JavaScript on Miren
description: Deploy Node.js and Bun apps — Express, Next.js, Elysia — on Miren with automatic build detection.
keywords: [javascript, typescript, node, nodejs, bun, express, nextjs, elysia, npm, yarn, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# JavaScript on Miren

Miren auto-detects both **Node.js** and **Bun** apps and builds a container image for
you — no Dockerfile required. It picks the right package manager from your lockfile
and runs the start command from your `package.json` or `Procfile`. TypeScript works
the same way; either build it during the image build or run it directly with Bun.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this app on Miren" after installing the
[Miren agent skills](/agent-skills). It detects Node vs. Bun, finds your start script,
wires up environment variables, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

No. Miren detects your project and builds the image automatically. Provide a
`Dockerfile.miren` only for custom build steps — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

## Node.js

**Detection:** `package.json` **and** a lockfile (`package-lock.json` or `yarn.lock`),
or a `Procfile` with a `web: node|npm|yarn` command. **Default version:** Node 20.

| Lockfile | Package manager | Install command |
|----------|-----------------|-----------------|
| `yarn.lock` | yarn | `yarn install` |
| `package-lock.json` | npm | `npm install` |

## Bun

**Detection:** `package.json` **and** `bun.lock`, or a `Procfile` with a `web: bun`
command. **Default version:** Bun 1. Bun can run TypeScript directly, so no separate
build step is needed for `.ts` entrypoints.

## Set up the app

From your project root:

<CliCommand context="client">
```miren
miren init
miren deploy
```
</CliCommand>

Preview the detected stack, package manager, and start command without building:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

### Start command

Your server must listen on `0.0.0.0` at the port in `$PORT` — Miren injects `PORT`
and routes traffic to it. Read it in code as `process.env.PORT` (Node) or
`Bun.env.PORT` / `process.env.PORT` (Bun). Miren runs your `package.json` start
script by default; a `Procfile` makes the command explicit:

```procfile
# Node — direct
web: node server.js

# Node — npm script
web: npm start

# Node — yarn script
web: yarn start

# Express — compiled output
web: node dist/index.js

# Next.js
web: npm run start

# Bun — run TypeScript directly
web: bun run src/index.ts

# Bun — run JavaScript
web: bun run src/index.js

# Bun — package.json script
web: bun run start

# Bun — Elysia
web: bun run src/server.ts

# Background worker (Node)
worker: node worker.js

# Background worker (Bun)
worker: bun run worker.ts
```

An Express server, for example, must bind the injected port:

```js
const port = process.env.PORT || 3000;
app.listen(port, "0.0.0.0", () => console.log(`listening on ${port}`));
```

### Building TypeScript / assets

For Node projects that compile TypeScript or bundle assets, run the build during the
image build with an `onbuild` step in `.miren/app.toml`:

```toml
[build]
onbuild = ["npm run build"]
```

Then point your start command at the compiled output (e.g. `web: node dist/index.js`).
Bun apps can skip this and run `.ts` files directly.

See [Services](/services) to run a worker alongside your web process.

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked
in output and logs):

<CliCommand context="client">
```miren
miren env set -e NODE_ENV=production
miren env set -s DATABASE_URL
miren env set -s SESSION_SECRET
```
</CliCommand>

`miren env set -s SESSION_SECRET` (no value) prompts with masked input. You can also
declare variables in `.miren/app.toml`:

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

- **Detection:** `package.json` + lockfile — `bun.lock` → Bun, `yarn.lock`/`package-lock.json` → Node. A `Procfile` with a `web: bun` or `web: node|npm|yarn` command also works in place of a lockfile
- **Default versions:** Node 20, Bun 1 (override via `[build] version` in `.miren/app.toml`)
- **Install:** `yarn install` / `npm install` / `bun install` by lockfile
- **Start command:** listen on `0.0.0.0:$PORT` via `process.env.PORT`; runs `package.json` start by default, or set a `Procfile`
- **TypeScript:** Node → build with `[build] onbuild`; Bun → runs `.ts` directly
- **Env vars:** `miren env set -e KEY=VALUE`, `-s` for secrets, or `[[env]]` in `app.toml`
- **Dockerfile:** not needed; add `Dockerfile.miren` only for custom builds

## Next steps

- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Services](/services) — web + workers
- [Deployment](/deployment) — how deploys build and activate
