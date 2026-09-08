---
title: R on Miren
description: Deploy R APIs on Miren with a Dockerfile.miren using Plumber.
keywords: [r, rlang, plumber, api, data science, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# R on Miren

R isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[Plumber](https://www.rplumber.io) to turn R functions into an HTTP API — a common way
to serve models and data-science code.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this R API on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds Plumber to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect R, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it. Define endpoints in a Plumber file and
run it on `0.0.0.0` at the injected port.

`plumber.R`:

```r
#* @get /
#* @serializer text
function() {
  "Hello from R on Miren!\n"
}
```

`entrypoint.R`:

```r
library(plumber)
port <- as.integer(Sys.getenv("PORT", "8080"))
pr("plumber.R") |> pr_run(host = "0.0.0.0", port = port)
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. The `rstudio/plumber` image already has
R and Plumber installed:

```dockerfile
FROM rstudio/plumber:latest

WORKDIR /app
COPY . /app

EXPOSE 8080
```

If you need extra packages, install them in the build:
`RUN R -e 'install.packages(c("DBI", "RPostgres"))'`.

### .dockerignore

```text
.git
```

## Set up the app

Add a `Procfile`:

```procfile
web: Rscript /app/entrypoint.R
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "r-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `Sys.getenv("KEY")`:

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
- **Base image:** `rstudio/plumber:latest` (R + Plumber preinstalled)
- **Service is required:** define a `Procfile` (`web: Rscript /app/entrypoint.R`) — the image `CMD` is not used
- **Port:** `Sys.getenv("PORT")`; `pr_run(host = "0.0.0.0", port = port)`
- **Env vars:** `miren env set -e/-s`; read with `Sys.getenv`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
