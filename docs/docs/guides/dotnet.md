---
title: .NET on Miren
description: Deploy ASP.NET Core apps on Miren with a Dockerfile.miren using the official .NET images.
keywords: [dotnet, .net, csharp, c#, aspnet, asp.net core, kestrel, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# .NET on Miren

.NET isn't auto-detected, so you deploy ASP.NET Core apps with a `Dockerfile.miren`
that publishes your app with the SDK image and runs it on the smaller ASP.NET runtime
image.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this .NET app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, points Kestrel at
`0.0.0.0:$PORT`, wires up environment variables, and deploys — using this page as its
reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect .NET, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so Kestrel must listen on `0.0.0.0` at
that port. The simplest way is to pass the URL to `app.Run`:

```csharp
var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

app.MapGet("/", () => "Hello from .NET on Miren!\n");

var port = Environment.GetEnvironmentVariable("PORT") ?? "8080";
app.Run($"http://0.0.0.0:{port}");
```

Reading `PORT` in code (as above) is the simplest approach. `ASPNETCORE_URLS` also works,
but Miren stores env vars literally and does **not** shell-expand `$PORT`, so you can't set
it to `http://0.0.0.0:$PORT` in `app.toml` — the app would receive that literal string. Use
it only from a shell entrypoint that expands `PORT` at runtime.

## The Dockerfile

Create `Dockerfile.miren` in your project root. Replace `dotnet-bench.dll` with your
project's assembly name:

```dockerfile
# ----- Build stage -----
FROM mcr.microsoft.com/dotnet/sdk:9.0 AS builder
WORKDIR /app
COPY . .
RUN dotnet publish -c Release -o /out

# ----- Runtime stage -----
FROM mcr.microsoft.com/dotnet/aspnet:9.0
WORKDIR /app
COPY --from=builder /out .
EXPOSE 8080
CMD ["dotnet", "dotnet-bench.dll"]
```

### .dockerignore

```text
.git
bin
obj
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "dotnet-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `Environment.GetEnvironmentVariable` or the
configuration system:

<CliCommand context="client">
```miren
miren env set -e ASPNETCORE_ENVIRONMENT=Production
miren env set -s ConnectionStrings__Default
```
</CliCommand>

.NET maps `__` in an env var name to nested configuration keys, so
`ConnectionStrings__Default` becomes `ConnectionStrings:Default`. You can also declare
variables in `.miren/app.toml`:

```toml
[[env]]
key = "ASPNETCORE_ENVIRONMENT"
value = "Production"
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Build:** `dotnet publish -c Release -o /out` on the SDK image; run on `dotnet/aspnet`
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `app.Run($"http://0.0.0.0:{port}")` (reading `PORT` in code; `ASPNETCORE_URLS` is not shell-expanded)
- **Env vars:** `miren env set -e/-s`; `__` maps to nested config keys
- **Database:** optional `[addons.miren-postgresql]` injects `DATABASE_URL`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Addons](/addons) — managed Postgres and other backing services
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
