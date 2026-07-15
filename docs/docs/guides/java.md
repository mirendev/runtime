---
title: Java on Miren
description: Deploy Spring Boot and other JVM apps on Miren with a Dockerfile.miren that builds a runnable jar.
keywords: [java, jvm, spring boot, kotlin, scala, clojure, maven, gradle, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Java on Miren

The JVM isn't auto-detected, so you deploy Java apps with a `Dockerfile.miren` that
builds a runnable jar and runs it on a JRE image. This guide uses Spring Boot with
Maven as the example; the same pattern works for Gradle, Kotlin, Scala, and Clojure —
build a jar, then `java -jar` it.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Spring Boot app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds the server to
`0.0.0.0:$PORT`, wires up environment variables, and deploys — using this page as its
reference.
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

Miren injects `PORT` and routes traffic to it. Spring Boot reads `server.port`, so map
`PORT` to it in `src/main/resources/application.properties`:

```properties
server.port=${PORT:8080}
server.address=0.0.0.0
```

`${PORT:8080}` uses the `PORT` environment variable when present and falls back to 8080
for local development. Frameworks other than Spring bind however they normally do —
the key is to read `PORT` and listen on `0.0.0.0`.

## The Dockerfile

Create `Dockerfile.miren` in your project root. The build caches dependencies before
copying source so rebuilds are faster:

```dockerfile
# ----- Build stage -----
FROM maven:3.9-eclipse-temurin-21 AS builder
WORKDIR /app
COPY pom.xml .
RUN mvn -q -B dependency:go-offline
COPY src src
RUN mvn -q -B -DskipTests package

# ----- Runtime stage -----
FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=builder /app/target/app.jar app.jar
EXPOSE 8080
CMD ["java", "-jar", "app.jar"]
```

Set `<finalName>app</finalName>` in your `pom.xml` `<build>` so the jar has a stable
name, or adjust the `COPY` to match your artifact.

### .dockerignore

```text
.git
target
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: java -jar /app/app.jar
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "java-bench"
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
output and logs). Spring maps env vars to properties (`SPRING_DATASOURCE_URL` →
`spring.datasource.url`):

<CliCommand context="client">
```miren
miren env set -e SPRING_PROFILES_ACTIVE=prod
miren env set -s SPRING_DATASOURCE_URL
```
</CliCommand>

You can also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "SPRING_PROFILES_ACTIVE"
value = "prod"
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Build:** `mvn -DskipTests package` (or Gradle); run the jar on a JRE image
- **Service is required:** define a `Procfile` (`web: java -jar /app/app.jar`) — the image `CMD` is not used
- **Port:** Spring `server.port=${PORT:8080}` + `server.address=0.0.0.0`; other frameworks read `PORT` and bind `0.0.0.0`
- **Env vars:** `miren env set -e/-s`; Spring maps `SPRING_*` env vars to properties
- **Database:** optional `[addons.miren-postgresql]` injects `DATABASE_URL`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Addons](/addons) — managed Postgres and other backing services
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
