---
title: Common Lisp on Miren
description: Deploy Common Lisp apps on Miren with a Dockerfile.miren using SBCL and Hunchentoot.
keywords: [common lisp, lisp, sbcl, hunchentoot, quicklisp, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Common Lisp on Miren

Common Lisp isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide
uses [SBCL](https://www.sbcl.org) with the [Hunchentoot](https://edicl.github.io/hunchentoot/)
web server, with dependencies pulled by Quicklisp.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Common Lisp app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, binds Hunchentoot
to `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Add a `Dockerfile.miren` to your project root. Miren builds from it instead of
guessing the stack — see [Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and bind `0.0.0.0`. The
script loads Quicklisp, defines a handler, starts the acceptor, and then blocks forever
so the process stays up:

```lisp
(load "/quicklisp/setup.lisp")
(ql:quickload :hunchentoot :silent t)

(setf hunchentoot:*dispatch-table*
      (list (hunchentoot:create-prefix-dispatcher
             "/"
             (lambda ()
               (setf (hunchentoot:content-type*) "text/plain")
               (format nil "Hello from Common Lisp on Miren!~%")))))

(defvar *port* (parse-integer (or (uiop:getenv "PORT") "8080")))
(hunchentoot:start
 (make-instance 'hunchentoot:easy-acceptor :port *port* :address "0.0.0.0"))
(format t "listening on 0.0.0.0:~a~%" *port*)
(loop (sleep 3600))
```

The final `(loop (sleep 3600))` matters — Hunchentoot's acceptor runs in a background
thread, so without it the script would finish and the process would exit.

## The Dockerfile

Create `Dockerfile.miren` in your project root. It installs Quicklisp and preloads
Hunchentoot during the build so startup is fast:

```dockerfile
FROM clfoundation/sbcl:latest

RUN apt-get update -y && apt-get install -y curl && rm -rf /var/lib/apt/lists/* \
    && curl -sO https://beta.quicklisp.org/quicklisp.lisp \
    && sbcl --non-interactive --load quicklisp.lisp \
        --eval '(quicklisp-quickstart:install :path "/quicklisp")' \
    && sbcl --non-interactive --load /quicklisp/setup.lisp \
        --eval '(ql:quickload :hunchentoot)'

WORKDIR /app
COPY . /app
EXPOSE 8080
CMD ["sbcl", "--disable-debugger", "--load", "/app/app.lisp"]
```

### .dockerignore

```text
.git
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "commonlisp-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `(uiop:getenv "KEY")`:

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
- **Base image:** `clfoundation/sbcl:latest`; install Quicklisp + preload Hunchentoot in the build
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Keep-alive:** end the script with `(loop (sleep 3600))` so the process stays running
- **Port:** `(uiop:getenv "PORT")`; `easy-acceptor :address "0.0.0.0"`
- **Env vars:** `miren env set -e/-s`; read with `(uiop:getenv "KEY")`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
