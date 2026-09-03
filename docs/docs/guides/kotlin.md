---
title: Kotlin on Miren
description: Deploy Kotlin apps on Miren with a Dockerfile.miren using Ktor.
keywords: [kotlin, ktor, netty, gradle, shadow jar, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Kotlin on Miren

Kotlin isn't auto-detected, so you deploy it with a `Dockerfile.miren` that builds a fat
jar and runs it on a JRE. This guide uses [Ktor](https://ktor.io) with its embedded
Netty server.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Ktor app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds the server to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect the JVM, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`:

```kotlin
import io.ktor.server.application.*
import io.ktor.server.engine.*
import io.ktor.server.netty.*
import io.ktor.server.response.*
import io.ktor.server.routing.*

fun main() {
    val port = System.getenv("PORT")?.toIntOrNull() ?: 8080
    embeddedServer(Netty, port = port, host = "0.0.0.0") {
        routing {
            get("/") { call.respondText("Hello from Kotlin on Miren!\n") }
        }
    }.start(wait = true)
}
```

A `build.gradle.kts` using the Shadow plugin to produce a fat jar:

```kotlin
plugins {
    kotlin("jvm") version "2.0.20"
    application
    id("com.gradleup.shadow") version "8.3.5"
}

repositories { mavenCentral() }

dependencies {
    implementation("io.ktor:ktor-server-netty:2.3.12")
}

application { mainClass.set("MainKt") }
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. The build runs `shadowJar` and the
runtime image copies the resulting `*-all.jar`:

```dockerfile
# ----- Build stage -----
FROM gradle:8.10-jdk21 AS builder
WORKDIR /app
COPY . .
RUN gradle shadowJar --no-daemon

# ----- Runtime stage -----
FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=builder /app/build/libs/*-all.jar app.jar
EXPOSE 8080
CMD ["java", "-jar", "app.jar"]
```

### .dockerignore

```text
.git
build
.gradle
```

## Set up the app

Add a `Procfile`:

```procfile
web: java -jar /app/app.jar
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "kotlin-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `System.getenv("KEY")`:

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
- **Build:** `gradle shadowJar` (fat jar) on `gradle:8.10-jdk21`; run on a JRE image
- **Service is required:** define a `Procfile` (`web: java -jar /app/app.jar`) — the image `CMD` is not used
- **Port:** `System.getenv("PORT")`; `embeddedServer(Netty, port, host = "0.0.0.0")`
- **Env vars:** `miren env set -e/-s`; read with `System.getenv`

## Next steps

- [Java on Miren](/guides/java) — the JVM sibling guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
