---
title: Julia on Miren
description: Deploy Julia web APIs on Miren with a Dockerfile.miren using HTTP.jl.
keywords: [julia, http.jl, genie, plumber, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Julia on Miren

Julia isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[HTTP.jl](https://github.com/JuliaWeb/HTTP.jl) directly; the same pattern works for
Genie or Oxygen.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Julia app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Julia, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`:

```julia
using HTTP

port = parse(Int, get(ENV, "PORT", "8080"))
println("listening on 0.0.0.0:$port")

HTTP.serve("0.0.0.0", port) do req
    HTTP.Response(200, "Hello from Julia on Miren!\n")
end
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. Install and precompile packages during
the build so startup is fast:

```dockerfile
FROM julia:1.10

WORKDIR /app
RUN julia -e 'using Pkg; Pkg.add("HTTP"); using HTTP'
COPY . /app

EXPOSE 8080
```

:::info[Cold-start compile]
Julia JIT-compiles on first use, so the first request after an instance starts is
slower than later ones. Precompiling in the build (`using HTTP`) reduces this. For
heavier apps, consider `PackageCompiler.jl` to bake a sysimage.
:::

### .dockerignore

```text
.git
```

## Set up the app

Add a `Procfile`:

```procfile
web: julia /app/app.jl
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "julia-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `ENV["KEY"]` or `get(ENV, "KEY", default)`:

<CliCommand context="client">
```miren
miren env set -e JULIA_NUM_THREADS=4
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
- **Base image:** `julia:1.10`; `Pkg.add` + `using` in the build to precompile
- **Service is required:** define a `Procfile` (`web: julia /app/app.jl`) — the image `CMD` is not used
- **Port:** `get(ENV, "PORT", "8080")`; `HTTP.serve("0.0.0.0", port)`
- **Cold start:** JIT compiles on first request; precompile in build or use `PackageCompiler.jl`
- **Env vars:** `miren env set -e/-s`; read with `ENV["KEY"]`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
