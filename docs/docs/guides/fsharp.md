---
title: F# on Miren
description: Deploy F# apps on Miren with a Dockerfile.miren using ASP.NET Core.
keywords: [fsharp, f#, dotnet, .net, aspnet, giraffe, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# F# on Miren

F# isn't auto-detected, so you deploy it with a `Dockerfile.miren` — the same .NET
toolchain as [C#](/guides/dotnet), just with F# source. This guide uses an ASP.NET Core
minimal API; the pattern also works for Giraffe and Saturn.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this F# app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, points Kestrel at
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

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
that port. Pass the URL to `app.Run`:

```fsharp
open System
open Microsoft.AspNetCore.Builder
open Microsoft.AspNetCore.Http

[<EntryPoint>]
let main args =
    let builder = WebApplication.CreateBuilder(args)
    let app = builder.Build()
    app.MapGet("/", Func<string>(fun () -> "Hello from F# on Miren!\n")) |> ignore
    let port = Environment.GetEnvironmentVariable("PORT")
    let port = if String.IsNullOrEmpty(port) then "8080" else port
    app.Run(sprintf "http://0.0.0.0:%s" port)
    0
```

An `.fsproj` using the web SDK:

```xml
<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup>
    <TargetFramework>net9.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.fs" />
  </ItemGroup>
</Project>
```

(F# compiles files in listed order, so keep `Program.fs` last.)

## The Dockerfile

Create `Dockerfile.miren` in your project root. Replace `fsharp-bench.dll` with your
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
CMD ["dotnet", "fsharp-bench.dll"]
```

### .dockerignore

```text
.git
bin
obj
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: dotnet /app/fsharp-bench.dll
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "fsharp-bench"
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
output and logs):

<CliCommand context="client">
```miren
miren env set -e ASPNETCORE_ENVIRONMENT=Production
miren env set -s ConnectionStrings__Default
```
</CliCommand>

`__` in an env var name maps to nested configuration keys. You can also declare
variables in `.miren/app.toml`:

```toml
[[env]]
key = "ASPNETCORE_ENVIRONMENT"
value = "Production"
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren` (same toolchain as [C#](/guides/dotnet))
- **Build:** `dotnet publish -c Release -o /out` on the SDK image; run on `dotnet/aspnet`
- **fsproj:** use `Microsoft.NET.Sdk.Web`; list `<Compile Include>` files in dependency order
- **Service is required:** `Procfile` `web: dotnet /app/<assembly>.dll` — the image `CMD` is not used
- **Port:** `app.Run(sprintf "http://0.0.0.0:%s" port)` (reading `PORT` in code; `ASPNETCORE_URLS` is not shell-expanded)
- **Env vars:** `miren env set -e/-s`; `__` maps to nested config keys

## Next steps

- [.NET on Miren](/guides/dotnet) — the C# sibling guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
