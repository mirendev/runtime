---
title: Language Guides
description: Step-by-step deployment guides for a wide range of languages — Python, JavaScript, Go, Ruby, Rust, Elixir, Gleam, Swift, Kotlin, COBOL, and many more — on Miren.
keywords: [guides, languages, python, javascript, node, bun, go, ruby, rust, elixir, gleam, dockerfile, build, deploy]
---

# Language Guides

These guides take you from a project on your laptop to a running app on Miren, one
language at a time. Each one covers the same three things: **how to set up the app**,
**how to set environment variables**, and **whether you need a Dockerfile**.

:::tip[Let your agent do this]
If you use an AI coding agent (Claude Code, Codex, Amp, and others), you don't have to
follow these guides by hand. Install the [Miren agent skills](/agent-skills) and ask
your agent to "set up this app on Miren" — it reads your project, detects the stack,
wires up environment variables, and deploys. These guides double as the reference your
agent works from. See [Agent Skills](/agent-skills) for setup.
:::

## Pick your language

| Guide | Auto-detected? | You provide |
|-------|----------------|-------------|
| [Python](/guides/python) | Yes | `requirements.txt` / `pyproject.toml` / `Pipfile` / `uv.lock` |
| [JavaScript (Node & Bun)](/guides/javascript) | Yes | `package.json` + a lockfile |
| [Go](/guides/go) | Yes | `go.mod` |
| [Ruby](/guides/ruby) | Yes | `Gemfile` |
| [Rust](/guides/rust) | Yes | `Cargo.toml` |
| [Java / JVM](/guides/java) | No | `Dockerfile.miren` |
| [PHP](/guides/php) | No | `Dockerfile.miren` |
| [.NET / C#](/guides/dotnet) | No | `Dockerfile.miren` |
| [C++](/guides/cpp) | No | `Dockerfile.miren` |
| [C](/guides/c) | No | `Dockerfile.miren` |
| [Deno](/guides/deno) | No | `Dockerfile.miren` |
| [Elixir](/guides/elixir) | No | `Dockerfile.miren` |
| [Kotlin](/guides/kotlin) | No | `Dockerfile.miren` |
| [Swift](/guides/swift) | No | `Dockerfile.miren` |
| [Dart](/guides/dart) | No | `Dockerfile.miren` |
| [Scala](/guides/scala) | No | `Dockerfile.miren` |
| [Clojure](/guides/clojure) | No | `Dockerfile.miren` |
| [Erlang](/guides/erlang) | No | `Dockerfile.miren` |
| [Haskell](/guides/haskell) | No | `Dockerfile.miren` |
| [F#](/guides/fsharp) | No | `Dockerfile.miren` |
| [Julia](/guides/julia) | No | `Dockerfile.miren` |
| [R](/guides/r) | No | `Dockerfile.miren` |
| [Lua](/guides/lua) | No | `Dockerfile.miren` |
| [Perl](/guides/perl) | No | `Dockerfile.miren` |
| [OCaml](/guides/ocaml) | No | `Dockerfile.miren` |
| [Crystal](/guides/crystal) | No | `Dockerfile.miren` |
| [Nim](/guides/nim) | No | `Dockerfile.miren` |
| [Zig](/guides/zig) | No | `Dockerfile.miren` |
| [Gleam](/guides/gleam) | No | `Dockerfile.miren` |
| [Objective-C](/guides/objc) | No | `Dockerfile.miren` |
| [Raku](/guides/raku) | No | `Dockerfile.miren` |
| [Common Lisp](/guides/commonlisp) | No | `Dockerfile.miren` |
| [JRuby](/guides/jruby) | No | `Dockerfile.miren` |
| [TruffleRuby](/guides/truffleruby) | No | `Dockerfile.miren` |
| [Klong (K)](/guides/klong) | No | `Dockerfile.miren` |
| [COBOL](/guides/cobol) | No | `Dockerfile.miren` |
| [Bash](/guides/bash) | No | `Dockerfile.miren` |
| [Static sites & SPAs](/guides/static) | No | `Dockerfile.miren` |

## Auto-detected vs. Dockerfile

Six stacks are auto-detected. Miren reads your project files, picks the build stack, and
builds a container image for you — **no Dockerfile required**. You run `miren init` once
and `miren deploy`, and Miren figures out the rest.

| Stack | Detected from | Default version |
|-------|---------------|-----------------|
| [Ruby](/guides/ruby) | `Gemfile` | 3.2 |
| [Python](/guides/python) | `requirements.txt`, `Pipfile`, `pyproject.toml`, or `uv.lock` | 3.11 |
| [Node.js](/guides/javascript) | `package.json` + `package-lock.json`/`yarn.lock` | 20 |
| [Bun](/guides/javascript) | `package.json` + `bun.lock` | 1 |
| [Go](/guides/go) | `go.mod` | Parsed from `go.mod`, else 1.23 |
| [Rust](/guides/rust) | `Cargo.toml` | 1.83 |

Each guide covers that stack's detection rules, build process, and start command in
full. Override any default version with `[build] version` in
[`.miren/app.toml`](/app-toml#build).

Every other language here — from Elixir and Gleam to Kotlin, Swift, Julia, and even
COBOL — isn't auto-detected, so its guide shows you a `Dockerfile.miren` you
can drop into your project. Miren builds from that Dockerfile instead of guessing. This
is the same escape hatch available to every language when you need full control over the
build.

:::tip[Want native support?]
Native builds cover the common stacks today (Python, Node, Bun, Go, Ruby, Rust). If you'd
like Miren to detect and build another language first-class — no Dockerfile needed —
[tell us what to build next](https://linear.miren.garden/suggest).
:::

## Using Dockerfile.miren {#using-dockerfilemiren}

For applications that require custom build steps or don't fit the automatic detection,
provide a `Dockerfile.miren` in your project root. Miren builds from it instead of
guessing your stack.

### When to use Dockerfile.miren

- Your application requires custom system dependencies
- You need a multi-stage build
- You're using a language that isn't auto-detected
- You need specific build-time configurations

### Example

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
EXPOSE 3000
CMD ["node", "dist/index.js"]
```

### Build priority

1. `build.dockerfile` setting in `app.toml` (if specified)
2. `Dockerfile.miren` in project root
3. Automatic language detection

### Build arguments

Miren passes the following build arguments to your `Dockerfile.miren`:

- `MIREN_VERSION` — the app version this build is producing, the same identifier
  [`miren app versions`](/command/app-versions) lists and the same value injected as the
  `MIREN_VERSION` environment variable at runtime

As with any Docker build argument, declare it with `ARG` in the stage that uses it:

```dockerfile
FROM alpine:3.19
ARG MIREN_VERSION
RUN echo "building $MIREN_VERSION"
```

Build arguments apply to `Dockerfile.miren` builds only — auto-detected stacks don't run
a Dockerfile.

:::info[Custom Dockerfiles need an explicit service]
Even with a `Dockerfile.miren`, you must define at least one service — a `Procfile` or a
`[services.web]` block. Miren does not use the image's `CMD`/`ENTRYPOINT` as the start
command; that fallback applies only to auto-detected stacks.
:::

## What every guide assumes

- You've installed Miren and can reach a cluster. If not, start with
  [Getting Started](/getting-started).

:::warning[Bind to the injected port]
Your web service must bind to `0.0.0.0` on the port in the `PORT` environment variable.
Miren injects `PORT` at runtime and routes traffic to it — an app that hardcodes
`localhost` or a fixed port won't receive traffic.
:::

## Next steps

- [app.toml Reference — Build](/app-toml#build) — `version`, `dockerfile`, and `onbuild` settings
- [Deployment](/deployment) — how `miren deploy` builds and activates versions
- [App Configuration](/app-configuration) — customize with `.miren/app.toml`
- [Services](/services) — run workers and multiple processes
- [Agent Skills](/agent-skills) — let your agent operate Miren for you
