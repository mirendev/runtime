---
title: C++ on Miren
description: Deploy C++ apps on Miren with a Dockerfile.miren using cpp-httplib.
keywords: [c++, cpp, httplib, cpp-httplib, gcc, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# C++ on Miren

C++ isn't auto-detected, so you deploy it with a `Dockerfile.miren` that compiles your
program and runs the binary. This guide uses the header-only
[cpp-httplib](https://github.com/yhirose/cpp-httplib) library.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this C++ app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, confirms the server
binds `0.0.0.0:$PORT`, and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Miren doesn't auto-detect C++, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## Bind to the injected port

Miren injects `PORT` and routes traffic to it, so read `PORT` and listen on `0.0.0.0`:

```cpp
#include "httplib.h"
#include <cstdlib>

int main() {
    httplib::Server svr;
    svr.Get("/", [](const httplib::Request &, httplib::Response &res) {
        res.set_content("Hello from C++ on Miren!\n", "text/plain");
    });
    const char *p = std::getenv("PORT");
    int port = p ? std::atoi(p) : 8080;
    if (!svr.listen("0.0.0.0", port)) {
        return 1;  // bind/startup failed — surface it to the deploy
    }
    return 0;
}
```

## The Dockerfile

Create `Dockerfile.miren` in your project root. It fetches the single `httplib.h` header
and compiles with pthreads:

```dockerfile
FROM gcc:14 AS build
WORKDIR /app
RUN apt-get update -y && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
COPY main.cpp .
RUN curl -sSL -o httplib.h https://raw.githubusercontent.com/yhirose/cpp-httplib/v0.18.3/httplib.h \
    && g++ -O2 -std=c++17 -pthread -o app main.cpp

FROM debian:trixie-slim
COPY --from=build /app/app /usr/local/bin/app
EXPOSE 8080
CMD ["app"]
```

:::warning[Match the runtime to the compiler image]
The `gcc:14` image is built on Debian trixie (glibc 2.38, a newer `libstdc++`). Running
the binary on an older base like `debian:12-slim` fails at startup with
`GLIBC_2.38 not found` / `GLIBCXX_3.4.32 not found`. Use a runtime base from the **same
or newer** Debian release (`debian:trixie-slim`), or statically link the C++ runtime
with `-static-libstdc++ -static-libgcc`.
:::

### .dockerignore

```text
.git
```

## Deploy

The Dockerfile's `CMD` starts the app. Miren uses it as the web service's startup default,
so you don't need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "cpp-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `std::getenv("KEY")`:

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s DATABASE_URL
```
</CliCommand>

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Build:** fetch `httplib.h`; `g++ -O2 -std=c++17 -pthread -o app main.cpp` on `gcc:14`
- **Runtime glibc/libstdc++:** use `debian:trixie-slim` (matches `gcc:14`) — older bases crash on `GLIBC_2.38`/`GLIBCXX_3.4.32`
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed
- **Port:** `std::getenv("PORT")`; `svr.listen("0.0.0.0", port)`
- **Env vars:** `miren env set -e/-s`; read with `std::getenv`

## Next steps

- [C on Miren](/guides/c) — the C sibling guide
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Deployment](/deployment) — how deploys build and activate
