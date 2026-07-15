---
title: C on Miren
description: Deploy a C program as a web service on Miren with a Dockerfile.miren using libmicrohttpd.
keywords: [c, libmicrohttpd, gnu, http, gcc, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# C on Miren

C isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles your
program and runs the binary. This guide uses
[GNU libmicrohttpd](https://www.gnu.org/software/libmicrohttpd/) — a small, embeddable
HTTP server library that's a stock Debian package (`libmicrohttpd-dev`), so there's
nothing to vendor or download.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this C app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect C, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it. `MHD_start_daemon` binds all interfaces
(`0.0.0.0`) on the given port; read `PORT` from the environment:

```c
#include <microhttpd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static enum MHD_Result on_request(void *cls, struct MHD_Connection *conn,
                                  const char *url, const char *method,
                                  const char *version, const char *upload_data,
                                  size_t *upload_data_size, void **con_cls) {
    const char *page = "Hello from C on Miren!\n";
    struct MHD_Response *response = MHD_create_response_from_buffer(
        strlen(page), (void *)page, MHD_RESPMEM_PERSISTENT);
    MHD_add_response_header(response, "Content-Type", "text/plain");
    enum MHD_Result ret = MHD_queue_response(conn, MHD_HTTP_OK, response);
    MHD_destroy_response(response);
    return ret;
}

int main(void) {
    const char *port_env = getenv("PORT");
    int port = port_env ? atoi(port_env) : 8080;

    struct MHD_Daemon *daemon = MHD_start_daemon(
        MHD_USE_INTERNAL_POLLING_THREAD, port, NULL, NULL,
        &on_request, NULL, MHD_OPTION_END);
    if (!daemon) { fprintf(stderr, "failed to start daemon\n"); return 1; }

    printf("listening on 0.0.0.0:%d\n", port);
    fflush(stdout);
    pause();
    MHD_stop_daemon(daemon);
    return 0;
}
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. Build and runtime both use `debian:12`,
so the library and glibc versions match — link against `libmicrohttpd-dev` at build
time and install the `libmicrohttpd12` runtime library:

```dockerfile
# ----- Build stage -----
FROM debian:12 AS build
RUN apt-get update -y && apt-get install -y gcc libmicrohttpd-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY server.c .
RUN gcc -O2 -o app server.c -lmicrohttpd

# ----- Runtime stage -----
FROM debian:12-slim
RUN apt-get update -y && apt-get install -y libmicrohttpd12 && rm -rf /var/lib/apt/lists/*
COPY --from=build /app/app /usr/local/bin/app
EXPOSE 8080
CMD ["app"]
```

:::tip[Keep the build and runtime Debian versions in step]
Because both stages use `debian:12`, the compiled binary and the runtime `libc`/library
versions match. If you build on a newer image (e.g. `gcc:14`, which tracks Debian
trixie) and run on an older base, the binary crashes at startup with
`version 'GLIBC_2.38' not found`. Match the two, or statically link.
:::

### .dockerignore

```text
.git
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. Add a `Procfile`:

```procfile
web: /usr/local/bin/app
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "c-bench"
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
output and logs). Read them with `getenv("KEY")`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s API_TOKEN
```
</CliCommand>

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Library:** GNU libmicrohttpd from Debian (`libmicrohttpd-dev` build, `libmicrohttpd12` runtime)
- **Build:** `gcc -O2 -o app server.c -lmicrohttpd`; keep build + runtime on the same Debian release
- **Service is required:** define a `Procfile` (`web: /usr/local/bin/app`) — the image `CMD` is not used
- **Port:** `getenv("PORT")`; `MHD_start_daemon` binds `0.0.0.0`
- **Env vars:** `miren env set -e/-s`; read with `getenv`

## Next steps

- [C++ on Miren](/guides/cpp) — the C++ sibling guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Deployment](/deployment) — how deploys build and activate
