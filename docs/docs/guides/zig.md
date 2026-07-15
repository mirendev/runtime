---
title: Zig on Miren
description: Deploy Zig apps on Miren with a Dockerfile.miren that cross-compiles a static binary.
keywords: [zig, static binary, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Zig on Miren

Zig isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles your
app to a single static binary and runs it on a minimal image.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Zig app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms your
server binds `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect Zig, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::info[Zig's standard library moves fast]
The example below is validated against **Zig 0.14**. The `std.net` and `std.http` APIs
change between releases — if you pin a different Zig version, expect to adjust the socket
code.
:::

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so your server must read `PORT` and
listen on `0.0.0.0`. A minimal listener using `std.net` (Zig 0.14):

```zig
// src/main.zig — build.zig points root_source_file here
const std = @import("std");

pub fn main() !void {
    const port_str = std.posix.getenv("PORT") orelse "8080";
    const port = try std.fmt.parseInt(u16, port_str, 10);
    const addr = try std.net.Address.parseIp("0.0.0.0", port);
    var server = try addr.listen(.{ .reuse_address = true });
    defer server.deinit();

    const body = "Hello from Zig on Miren!\n";
    while (true) {
        const conn = server.accept() catch continue;
        defer conn.stream.close();
        var buf: [1024]u8 = undefined;
        _ = conn.stream.read(&buf) catch 0;
        var out: [256]u8 = undefined;
        const resp = std.fmt.bufPrint(&out, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: {d}\r\nConnection: close\r\n\r\n{s}", .{ body.len, body }) catch continue;
        conn.stream.writeAll(resp) catch {};
    }
}
```

A matching `build.zig`:

```zig
const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const exe = b.addExecutable(.{
        .name = "zigapp",
        .root_source_file = b.path("src/main.zig"),
        .target = target,
        .optimize = optimize,
    });
    b.installArtifact(exe);
}
```

## The Dockerfile

The official Zig release is a tarball, so the build stage downloads it and cross-compiles
a static musl binary:

```dockerfile
ARG ZIG_VERSION=0.14.0

# ----- Build stage -----
FROM alpine:3.20 AS builder
RUN apk add --no-cache curl xz
ARG ZIG_VERSION
RUN curl -sSL https://ziglang.org/download/${ZIG_VERSION}/zig-linux-x86_64-${ZIG_VERSION}.tar.xz \
      | tar -xJ -C /opt \
    && ln -s /opt/zig-linux-x86_64-${ZIG_VERSION}/zig /usr/local/bin/zig
WORKDIR /app
COPY . .
RUN zig build -Doptimize=ReleaseSafe -Dtarget=x86_64-linux-musl

# ----- Runtime stage -----
FROM alpine:3.20
COPY --from=builder /app/zig-out/bin/zigapp /usr/local/bin/app
EXPOSE 8080
CMD ["app"]
```

Miren's cluster builds on `x86_64`, so the download URL and `-Dtarget` use `x86_64`.

:::tip[Verify the tarball for production]
This pipes the release tarball straight into `tar` without checking it. For images you'll
run in production, verify the download against Zig's published SHA256 (listed alongside
each release at [ziglang.org/download](https://ziglang.org/download)) before extracting it.
:::

### .dockerignore

```text
.git
zig-out
.zig-cache
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: /usr/local/bin/app
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "zig-bench"
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
output and logs). Read them with `std.posix.getenv("KEY")`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s API_TOKEN
```
</CliCommand>

You can also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "API_TOKEN"
value = ""
required = true
sensitive = true
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren` (static binary)
- **Build:** download Zig tarball, `zig build -Doptimize=ReleaseSafe -Dtarget=x86_64-linux-musl`
- **Runtime:** copy `zig-out/bin/<name>` to a minimal Alpine image
- **Service is required:** define a `Procfile` (`web: /usr/local/bin/app`) — the image `CMD` is not used
- **Port:** read `std.posix.getenv("PORT")`; bind `0.0.0.0`
- **Std API churn:** pin a Zig version; `std.net`/`std.http` change between releases

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
