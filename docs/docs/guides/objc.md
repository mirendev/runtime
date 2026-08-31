---
title: Objective-C on Miren
description: Deploy an Objective-C web app on Miren with a Dockerfile.miren using GNUstep and SOPE.
keywords: [objective-c, objc, gnustep, sope, webobjects, ngobjweb, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Objective-C on Miren

Objective-C isn't auto-detected, so you deploy it with a `Dockerfile.miren` that
compiles your app with [GNUstep](https://www.gnustep.org). This guide uses
[SOPE](https://github.com/inverse-inc/sope) (the SKYRiX Object Publishing Environment —
the Objective-C web framework that SOGo is built on), whose `WOApplication` and
`WOHttpAdaptor` give you a real HTTP server, WebObjects-style.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Objective-C app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren` and GNUstep build,
and deploys — using this page as its reference.
:::

## Does this source build need a Dockerfile?

Yes. Add a `Dockerfile.miren` to your project root. Miren builds from it instead of
guessing the stack — see [Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## The app

Subclass `WOApplication` and override `dispatchRequest:` to return a `WOResponse`.
`WOApplicationMain` starts the app and its built-in HTTP adaptor:

```objc
#import <NGObjWeb/WOApplication.h>
#import <NGObjWeb/WOResponse.h>
#import <NGObjWeb/WORequest.h>
#import <NGObjWeb/WOCoreApplication.h>

@interface HelloApp : WOApplication
@end

@implementation HelloApp
- (WOResponse *)dispatchRequest:(WORequest *)_request {
    WOResponse *r = [WOResponse responseWithRequest:_request];
    [r setStatus:200];
    [r setHeader:@"text/plain" forKey:@"content-type"];
    [r appendContentString:@"Hello from Objective-C on Miren!\n"];
    return r;
}
@end

int main(int argc, const char *argv[]) {
    return WOApplicationMain(@"HelloApp", argc, argv);
}
```

## Bind to the injected port

SOPE's HTTP adaptor takes its listen address from the `WOPort` default, which you pass
on the command line. Miren injects `PORT`, so start the app with
`-WOPort 0.0.0.0:$PORT`. The Dockerfile's `CMD` below supplies that command.

:::warning[Give WOPort an explicit `0.0.0.0`]
`-WOPort 8080` alone makes the adaptor bind the wildcard address (`*:8080`), which fails
on this stack with `NGCouldNotBindSocketException … Address family not supported`. Pass
the address explicitly — `-WOPort 0.0.0.0:$PORT` — so it binds IPv4 on all interfaces.
The `sh -c '. GNUstep.sh && exec …'` wrapper sources the GNUstep environment so the app
finds its frameworks at runtime.
:::

## The Dockerfile

Build with GNUstep's makefile system. `gnustep-make` provides only the makefiles, so
install GNU `make` too; `libsope-dev` provides SOPE's headers and libraries. A
single-stage image keeps the GNUstep runtime and SOPE libraries available at run time:

```dockerfile
FROM debian:12
RUN apt-get update -y \
    && apt-get install -y make gobjc gnustep-make gnustep-base-runtime libgnustep-base-dev libsope-dev libsope1 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY main.m GNUmakefile ./
RUN . /usr/share/GNUstep/Makefiles/GNUstep.sh && make
EXPOSE 8080
CMD ["sh", "-c", ". /usr/share/GNUstep/Makefiles/GNUstep.sh && exec /app/obj/app -WOPort 0.0.0.0:$PORT"]
```

The `GNUmakefile` builds a plain tool linked against the SOPE libraries (SOPE's own
`woapp.make` bundle fragment is too old for current gnustep-make, so link them directly):

```makefile
include $(GNUSTEP_MAKEFILES)/common.make

TOOL_NAME = app
app_OBJC_FILES = main.m
app_TOOL_LIBS += -lNGObjWeb -lNGExtensions -lEOControl -lNGStreams -lNGMime -lSaxObjC -lDOM

include $(GNUSTEP_MAKEFILES)/tool.make
```

The compiled binary lands at `obj/app`.

### .dockerignore

```text
.git
```

## Deploy

Miren uses the image's `CMD` and infers the web port from `EXPOSE 8080`, so you don't
need a `Procfile` or service command.

Create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "objc-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Framework:** SOPE (`libsope-dev`) — `WOApplication` + `WOHttpAdaptor`; override `dispatchRequest:`
- **Build:** GNUstep makefiles as a plain tool linking `-lNGObjWeb …` (install `make`); binary at `obj/app`
- **Port:** `-WOPort 0.0.0.0:$PORT` — the explicit `0.0.0.0` avoids a wildcard bind failure
- **Runtime:** source `GNUstep.sh` before exec so the app finds its frameworks
- **Startup:** inherited from the Dockerfile `CMD`; no `Procfile` or service command needed

## Next steps

- [C on Miren](/guides/c) — the C guide (Objective-C is a superset of C)
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Deployment](/deployment) — how deploys build and activate
