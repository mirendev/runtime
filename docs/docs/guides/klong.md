---
title: Klong (K) on Miren
description: Deploy the K-like array language Klong on Miren with a Dockerfile.miren using KlongPy and socat.
keywords: [klong, k, array language, klongpy, apl, socat, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Klong (K) on Miren

[Klong](https://t3x.org/klong/) is an open-source array language in the K/APL family.
Array languages don't ship HTTP servers, so — as with [COBOL](/guides/cobol) and
[Bash](/guides/bash) — this guide has the Klong program print an HTTP response and puts
`socat` in front of it to own the socket. It uses [KlongPy](https://klongpy.org), a
pip-installable Klong implementation.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Klong app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren` and the socket
front-end, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Add a `Dockerfile.miren` to your project root. Miren builds from it instead of
guessing the stack — see [Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## The program

`.d` writes a string to standard output verbatim. Put a complete HTTP response —
status line, headers, a blank line, then the body — in `hello.kg`:

```klong
.d("HTTP/1.1 200 OK
Content-Type: text/plain
Connection: close

Hello from Klong on Miren!
")
```

:::note[Run Klong from a file for clean output]
Run the program as a **file** (`kgpy hello.kg`), not with `-e` or over a pipe — those
start the REPL and echo the expression result (`"…"` with quotes) alongside your
output. In file mode only your `.d` bytes are written.
:::

## The socket front-end

The program doesn't bind a port — `socat` does. Miren injects `PORT`, and `socat`'s
`TCP-LISTEN` binds all interfaces; `fork` runs Klong once per connection. The Dockerfile's
`CMD` starts that socket front-end.

:::note[Behind Miren's ingress]
Miren's HTTP ingress terminates TLS and handles the public HTTP layer in front of your
app, so `socat` only needs to hand each accepted connection to your program — it isn't
exposed to raw internet traffic. The one practical limit is that `fork` spawns a process
per request, so this suits low-traffic endpoints and tooling rather than high-throughput
services.
:::

## The Dockerfile

Create `Dockerfile.miren` in your project root. KlongPy installs from PyPI (it also needs
`colorama` for its CLI), and `socat` comes from apt:

```dockerfile
FROM python:3.12-slim
RUN pip install --no-cache-dir klongpy colorama \
    && apt-get update -y && apt-get install -y socat && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY hello.kg /app/
EXPOSE 8080
CMD ["sh", "-c", "exec socat TCP-LISTEN:$PORT,reuseaddr,fork EXEC:'kgpy /app/hello.kg'"]
```

### .dockerignore

```text
.git
```

## Deploy

Miren uses the image's `CMD` and infers the web port from `EXPOSE 8080`, so you don't
need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "klong-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Runtime:** KlongPy (`pip install klongpy colorama`) on `python:3.12-slim`
- **Serving:** the Klong program `.d`-prints a full HTTP response; `socat` owns the socket
- **Run from a file:** `kgpy hello.kg` (file mode) — `-e`/pipe modes echo the result too
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `socat TCP-LISTEN:$PORT` binds `0.0.0.0`

## Next steps

- [COBOL on Miren](/guides/cobol) and [Bash on Miren](/guides/bash) — the same `socat` pattern
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Deployment](/deployment) — how deploys build and activate
