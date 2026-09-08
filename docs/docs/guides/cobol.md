---
title: COBOL on Miren
description: Yes, really — deploy a COBOL program as a web service on Miren with a Dockerfile.miren.
keywords: [cobol, gnucobol, socat, cgi, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# COBOL on Miren

If it compiles to a program that can write bytes to a socket, Miren can run it — COBOL
included. COBOL has no built-in HTTP server, so this guide compiles a COBOL program with
[GnuCOBOL](https://gnucobol.sourceforge.io) that prints a complete HTTP response, and
puts `socat` in front of it to handle the socket.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this COBOL program on Miren" after installing the
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

The COBOL program writes a full HTTP response — status line, headers, a blank line, then
the body — to standard output. `X"0D0A"` is CRLF:

```cobol
       IDENTIFICATION DIVISION.
       PROGRAM-ID. hello.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 CRLF PIC X(2) VALUE X"0D0A".
       PROCEDURE DIVISION.
           DISPLAY
               "HTTP/1.1 200 OK" CRLF
               "Content-Type: text/plain" CRLF
               "Connection: close" CRLF
               CRLF
               "Hello from COBOL on Miren!" CRLF
               WITH NO ADVANCING
           STOP RUN.
```

## The socket front-end

The program itself doesn't listen on a port — `socat` does. For each connection, `socat`
runs the program and pipes its stdout back to the client. Miren injects `PORT`, and
`socat`'s `TCP-LISTEN` binds all interfaces. The Dockerfile's `CMD` starts that socket
front-end.

This `socat` pattern works for any language that can print an HTTP response to stdout.

:::note[Behind Miren's ingress]
Miren's HTTP ingress terminates TLS and handles the public HTTP layer in front of your
app, so `socat` only needs to hand each accepted connection to your program — it isn't
exposed to raw internet traffic. The one practical limit is that `fork` spawns a process
per request, so this suits low-traffic endpoints and tooling rather than high-throughput
services.
:::

## The Dockerfile

Create `Dockerfile.miren` in your project root. It installs GnuCOBOL and `socat`, then
compiles the program:

```dockerfile
FROM debian:12-slim

RUN apt-get update -y \
    && apt-get install -y gnucobol socat \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY hello.cob .
RUN cobc -x -o /app/hello hello.cob

EXPOSE 8080
CMD ["sh", "-c", "exec socat TCP-LISTEN:$PORT,reuseaddr,fork EXEC:/app/hello"]
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
name = "cobol-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Build:** `cobc -x -o /app/hello hello.cob` on `debian:12-slim` with `gnucobol`
- **Serving:** the program prints a full HTTP response; `socat` handles the socket
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `socat TCP-LISTEN:$PORT` binds `0.0.0.0`
- **Pattern:** the same `socat` front-end works for any language that writes an HTTP response to stdout

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
