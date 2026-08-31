---
title: Swift on Miren
description: Deploy server-side Swift apps on Miren with a Dockerfile.miren using Vapor.
keywords: [swift, vapor, server-side swift, swiftpm, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Swift on Miren

Swift isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles your
app and runs the binary. This guide uses [Vapor](https://vapor.codes), the main
server-side Swift framework.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Vapor app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds the server to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect Swift, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so set Vapor's server hostname to
`0.0.0.0` and the port from the environment:

```swift
import Vapor

var env = try Environment.detect()
let app = Application(env)
defer { app.shutdown() }

app.get { req in "Hello from Swift on Miren!\n" }

app.http.server.configuration.hostname = "0.0.0.0"
app.http.server.configuration.port = Int(Environment.get("PORT") ?? "8080") ?? 8080

try app.run()
```

A `Package.swift` declaring Vapor and an executable target:

```swift
// swift-tools-version:5.10
import PackageDescription

let package = Package(
    name: "swift-bench",
    platforms: [.macOS(.v13)],
    dependencies: [
        .package(url: "https://github.com/vapor/vapor.git", from: "4.106.0"),
    ],
    targets: [
        .executableTarget(
            name: "App",
            dependencies: [.product(name: "Vapor", package: "vapor")]
        ),
    ]
)
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. `--static-swift-stdlib` links the Swift
runtime into the binary so the runtime image stays small:

```dockerfile
# ----- Build stage -----
FROM swift:5.10-jammy AS build
WORKDIR /app
COPY . .
RUN swift build -c release --static-swift-stdlib

# ----- Runtime stage -----
FROM ubuntu:jammy
RUN apt-get update -y && apt-get install -y ca-certificates libcurl4 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /app/.build/release/App /app/App
EXPOSE 8080
CMD ["/app/App"]
```

:::info[Builds are slow]
Vapor pulls in swift-crypto, which compiles BoringSSL from C/C++ — the first build takes
several minutes. Miren caches image layers, so rebuilds that don't change dependencies
are faster.
:::

### .dockerignore

```text
.git
.build
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "swift-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `Environment.get("KEY")`:

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
- **Build:** `swift build -c release --static-swift-stdlib` on `swift:5.10-jammy` (slow — BoringSSL)
- **Runtime:** `ubuntu:jammy` + `ca-certificates libcurl4`; binary at `.build/release/<target>`
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `Environment.get("PORT")`; set `app.http.server.configuration.hostname = "0.0.0.0"`
- **Env vars:** `miren env set -e/-s`; read with `Environment.get`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
