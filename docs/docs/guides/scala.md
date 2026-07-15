---
title: Scala on Miren
description: Deploy Scala apps on Miren with a Dockerfile.miren using scala-cli and Cask.
keywords: [scala, cask, http4s, play, scala-cli, sbt, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Scala on Miren

Scala isn't auto-detected, so you deploy it with a `Dockerfile.miren` that packages a
runnable jar. This guide uses [scala-cli](https://scala-cli.virtuslab.org) with the
[Cask](https://com-lihaoyi.github.io/cask/) framework — the least-ceremony path. The
same jar-and-`java -jar` pattern works for sbt builds with http4s or Play.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Scala app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds the server to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Scala, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`. A Cask
app declares its `port` and `host`:

```scala
//> using dep com.lihaoyi::cask:0.9.4

object App extends cask.MainRoutes {
  override def port = sys.env.getOrElse("PORT", "8080").toInt
  override def host = "0.0.0.0"

  @cask.get("/")
  def hello() = "Hello from Scala on Miren!\n"

  initialize()
}
```

The `//> using dep` line lets scala-cli resolve dependencies without a separate build
file.

## The Dockerfile

Create `Dockerfile.miren` in your project root. scala-cli packages an assembly jar,
which runs on a plain JRE:

```dockerfile
# ----- Build stage -----
FROM virtuslab/scala-cli:latest AS builder
WORKDIR /app
COPY . .
RUN scala-cli --power package app.scala -o app.jar --assembly

# ----- Runtime stage -----
FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=builder /app/app.jar /app/app.jar
EXPOSE 8080
CMD ["java", "-jar", "app.jar"]
```

### .dockerignore

```text
.git
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: java -jar /app/app.jar
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "scala-bench"
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
output and logs). Read them with `sys.env.get("KEY")`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s DATABASE_URL
```
</CliCommand>

You can also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "DATABASE_URL"
value = ""
required = true
sensitive = true
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Build:** `scala-cli --power package app.scala -o app.jar --assembly`; run on a JRE image
- **Service is required:** define a `Procfile` (`web: java -jar /app/app.jar`) — the image `CMD` is not used
- **Port:** `sys.env.getOrElse("PORT", "8080").toInt`; Cask `override def host = "0.0.0.0"`
- **Env vars:** `miren env set -e/-s`; read with `sys.env.get`

## Next steps

- [Java on Miren](/guides/java) — the JVM sibling guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
