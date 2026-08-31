---
title: Bash on Miren
description: Yes, you can deploy a Bash script as a web service on Miren with a Dockerfile.miren.
keywords: [bash, shell, socat, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Bash on Miren

Bash can't listen on a socket by itself, but it can write an HTTP response to standard
output — and that's all you need. This guide puts `socat` in front of a Bash script:
`socat` owns the socket, and runs the script for each connection.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this shell script on Miren" after installing the
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

## The script

The script prints a complete HTTP response — status line, headers, a blank line, then
the body:

```bash
#!/usr/bin/env bash
printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nHello from Bash on Miren!\n'
```

## The socket front-end

The script doesn't bind a port — `socat` does. Miren injects `PORT`, and `socat`'s
`TCP-LISTEN` binds all interfaces; `fork` runs the script once per connection. The
Dockerfile's `CMD` starts that socket front-end.

This same `socat` pattern serves any program that writes an HTTP response to stdout —
see the [COBOL guide](/guides/cobol) for another example.

:::note[Behind Miren's ingress]
Miren's HTTP ingress terminates TLS and handles the public HTTP layer in front of your
app, so `socat` only needs to hand each accepted connection to your program — it isn't
exposed to raw internet traffic. The one practical limit is that `fork` spawns a process
per request, so this suits low-traffic endpoints and tooling rather than high-throughput
services.
:::

## The Dockerfile

Create `Dockerfile.miren` in your project root:

```dockerfile
FROM debian:12-slim
RUN apt-get update -y && apt-get install -y bash socat && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY hello.sh .
RUN chmod +x hello.sh
EXPOSE 8080
CMD ["sh", "-c", "exec socat TCP-LISTEN:$PORT,reuseaddr,fork EXEC:/app/hello.sh"]
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
name = "bash-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Serving:** the script prints a full HTTP response; `socat` owns the socket
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `socat TCP-LISTEN:$PORT` binds `0.0.0.0`
- **Pattern:** works for any executable that writes an HTTP response to stdout

## Next steps

- [COBOL on Miren](/guides/cobol) — the same `socat` pattern for a compiled program
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Deployment](/deployment) — how deploys build and activate
