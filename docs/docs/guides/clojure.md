---
title: Clojure on Miren
description: Deploy Clojure apps on Miren with a Dockerfile.miren using Ring and Jetty.
keywords: [clojure, ring, jetty, deps.edn, tools.deps, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Clojure on Miren

Clojure isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[Ring](https://github.com/ring-clojure/ring) with the Jetty adapter, run via the
`clojure` CLI and `deps.edn`.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Clojure app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds Jetty to
`0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect Clojure, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`:

```clojure
;; src/app.clj — deps.edn searches "src" and the image CMD runs the `app` namespace
(ns app
  (:require [ring.adapter.jetty :as jetty]))

(defn handler [_]
  {:status 200
   :headers {"Content-Type" "text/plain"}
   :body "Hello from Clojure on Miren!\n"})

(defn -main []
  (let [port (Integer/parseInt (or (System/getenv "PORT") "8080"))]
    (jetty/run-jetty handler {:port port :host "0.0.0.0" :join? true})))
```

A `deps.edn` with the source path and dependencies:

```clojure
{:paths ["src"]
 :deps {ring/ring-core {:mvn/version "1.12.2"}
        ring/ring-jetty-adapter {:mvn/version "1.12.2"}}}
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. `clojure -P` downloads dependencies at
build time so they're cached in the image:

```dockerfile
FROM clojure:temurin-21-tools-deps

WORKDIR /app
COPY deps.edn .
RUN clojure -P
COPY . /app

EXPOSE 8080
CMD ["clojure", "-M", "-m", "app"]
```

### .dockerignore

```text
.git
.cpcache
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "clojure-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

For faster startup you can instead build an uberjar (via `tools.build` or `depstar`) and
run `java -jar`, but running the `clojure` CLI directly with cached deps works fine.

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `(System/getenv "KEY")`:

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
- **Base image:** `clojure:temurin-21-tools-deps`; `clojure -P` caches deps in the build
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `(System/getenv "PORT")`; `run-jetty handler {:host "0.0.0.0"}`
- **Env vars:** `miren env set -e/-s`; read with `(System/getenv "KEY")`

## Next steps

- [Java on Miren](/guides/java) — the JVM sibling guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
