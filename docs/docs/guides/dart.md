---
title: Dart on Miren
description: Deploy Dart server apps on Miren with a Dockerfile.miren that compiles a native executable.
keywords: [dart, shelf, flutter backend, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Dart on Miren

Dart isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles a
native executable and runs it on a minimal image. This guide uses the
[shelf](https://pub.dev/packages/shelf) server stack.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Dart app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect Dart, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`:

```dart
import 'dart:io';
import 'package:shelf/shelf.dart';
import 'package:shelf/shelf_io.dart' as shelf_io;

void main() async {
  final port = int.parse(Platform.environment['PORT'] ?? '8080');
  handler(Request req) => Response.ok('Hello from Dart on Miren!\n');
  await shelf_io.serve(handler, '0.0.0.0', port);
  print('listening on 0.0.0.0:$port');
}
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. `dart compile exe` produces a native
binary:

```dockerfile
# ----- Build stage -----
FROM dart:stable AS builder
WORKDIR /app
COPY pubspec.* ./
RUN dart pub get
COPY . .
RUN dart pub get --offline && dart compile exe bin/server.dart -o /app/server

# ----- Runtime stage (glibc for the compiled exe) -----
FROM debian:12-slim
RUN apt-get update -y && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/server /app/server
EXPOSE 8080
CMD ["/app/server"]
```

:::warning[Run the compiled exe on glibc, not scratch]
`dart compile exe` produces a dynamically-linked binary that needs a C library at
runtime. Running it on `scratch` fails to boot; use a small glibc image like
`debian:12-slim`. (Copying Dart's `/runtime/` directory is only needed for AOT
snapshots run with `dartaotruntime`, not for `compile exe`.)
:::

### .dockerignore

```text
.git
.dart_tool
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "dart-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `Platform.environment['KEY']`:

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

- **Detection:** none — requires `Dockerfile.miren` (compiled exe)
- **Build:** `dart compile exe bin/server.dart -o /app/server` on `dart:stable`
- **Runtime:** `debian:12-slim` (glibc) — the compiled exe won't run on `scratch`
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `Platform.environment['PORT']`; bind `0.0.0.0` via `shelf_io.serve`
- **Env vars:** `miren env set -e/-s`; read with `Platform.environment`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
