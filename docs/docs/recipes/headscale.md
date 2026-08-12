---
title: Run a headscale control server
description: A worked example — self-host headscale, the open-source Tailscale control server, as a Miren app with its state on a persistent disk and its clients logging in over your own hostname.
keywords: [headscale, tailscale, tailnet, control server, vpn, wireguard, derp, self-hosted, example, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Run a headscale control server

[headscale](https://headscale.net) is an open-source implementation of the Tailscale
control server — the coordination plane your Tailscale clients log in to, exchange keys
through, and get their network map from. Self-hosting it means your tailnet's coordination
belongs to you.

The end state: headscale running as a single Miren `web` service on port 8080, reachable
over HTTPS at a hostname you own, with its SQLite database and private keys on a persistent
disk. You point `tailscale up --login-server` at it and register nodes with
`miren sandbox exec`.

Along the way it exercises a fair amount of Miren — running an upstream image on its own
entrypoint, a local disk, environment-driven config, and (for the embedded relay) a UDP node
port.

:::info[This is an application recipe, not a language guide]
For getting your own source code onto Miren, start with [Deployment](/deployment) and the
[Language Guides](/guides). This page is about self-hosting a prebuilt third-party server.
For the opposite topic — running a Miren **cluster** on a tailnet — see
[Running Miren on a Tailnet](/tailscale).
:::

## How it works

1. A three-line Dockerfile adds a `config.yaml` and a `CMD` to the official headscale image.
   The service sets no `command`, so the image's own `ENTRYPOINT` + `CMD` run as the process
   argv — which is what lets a shell-less image work at all.
2. The `web` service listens on `0.0.0.0:8080`. Miren's ingress terminates TLS at your
   hostname and proxies to it.
3. A `config.yaml` baked into the image holds everything static; the values that change per
   deployment come from `HEADSCALE_*` environment variables in `app.toml`.
4. The database and the noise private key live on a local disk at `/data`, so they survive
   redeploys.
5. Admin commands (`headscale users create` and friends) run *inside the live sandbox*,
   because the headscale CLI talks to the running server over a unix socket.

## Prerequisites

- `miren` CLI installed and authenticated (`miren whoami`).
- Access to the target cluster and its org.
- A hostname you control, pointed at the cluster — see
  [Custom Domains](/traffic-routing#custom-domains) or claim one through
  [Miren Cloud subdomains](/miren-cloud/subdomains).

## Select the target cluster

<CliCommand context="client">

```bash
# Add a cluster your cloud identity can see (interactive picker; pins the TLS fingerprint)
miren cluster add

miren whoami
```

</CliCommand>

## The Dockerfile

The official headscale image is **distroless** — it contains no shell, no package manager,
nothing but the binary at `/ko-app/headscale` and a CA bundle. That's fine, as long as you
don't ask Miren to run a `command`:

```dockerfile
FROM docker.io/headscale/headscale:0.29.3

COPY config.yaml /etc/headscale/config.yaml

# The image sets ENTRYPOINT ["/ko-app/headscale"] but no CMD, so give it one.
CMD ["serve"]
```

That's the whole thing. The upstream image already ships the CA bundle headscale needs to
fetch the DERP map, so there's nothing to install.

:::info[Why there's no `command` in the app.toml]
When a service sets no `command`, Miren runs the image's `ENTRYPOINT` + `CMD` directly as
the process argv, the way `docker run IMAGE` does — no shell involved, the entrypoint gets
PID 1, and signals reach it. Set a `command` and Miren runs it through `/bin/sh -c` instead,
which a distroless image has no shell for. That's the whole reason this recipe leaves
`command` out.
:::

Add a `.dockerignore` so local files stay out of the build context:

```text
.env
.miren
```

## The config.yaml

This sits next to the Dockerfile and gets baked into the image. Everything that varies per
deployment is overridden by an environment variable, so the file itself is the same for
everyone:

```yaml
# Overridden by HEADSCALE_SERVER_URL from app.toml.
server_url: http://127.0.0.1:8080

# Bind on all interfaces: Miren health-checks and routes from outside the container,
# so a 127.0.0.1 listener never comes up healthy.
listen_addr: 0.0.0.0:8080
metrics_listen_addr: 0.0.0.0:9090

# The CLI reaches the running server over this socket.
unix_socket: /var/run/headscale/headscale.sock

# State on the mounted disk. headscale creates both on first start, and opens
# SQLite in WAL mode on its own.
noise:
  private_key_path: /data/noise_private.key
database:
  type: sqlite
  sqlite:
    path: /data/db.sqlite

prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48

# Relay through Tailscale's public DERP servers.
derp:
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  auto_update_enabled: true
  update_frequency: 24h

dns:
  magic_dns: true
  # Your server_url host must not sit underneath this domain.
  base_domain: ts.example.com
  override_local_dns: false
  nameservers:
    global:
      - 1.1.1.1
      - 8.8.8.8
```

headscale honors `HEADSCALE_*` overrides for nested keys too — `database.sqlite.path`
becomes `HEADSCALE_DATABASE_SQLITE_PATH` — so you can move any of this into `app.toml`
without a boot script.

:::warning[A missing `dns` block fails validation]
`dns.override_local_dns` defaults to true, and headscale refuses to start without
nameservers to go with it:
`Fatal config error: dns.nameservers.global must be set when dns.override_local_dns is true`.
Either set `override_local_dns: false` as above, or supply `dns.nameservers.global`.
:::

## The app.toml

This file lives at `.miren/app.toml`.

```toml
name = "headscale"

[build]
dockerfile = "Dockerfile"

# Must be named `web`: `miren route set` has no port selector and always sends a
# hostname to the service called `web`.
#
# Deliberately no `command` — see above. The image's ENTRYPOINT + CMD run as-is.
[services.web]
port_timeout = "120s"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

[[services.web.ports]]
port = 8080
name = "http"
type = "http"

# The database, the noise key, and (if you enable it) the DERP key.
[[services.web.disks]]
name = "state"
provider = "local"
mount_path = "/data"

# The public URL clients log in to. This must match the routed hostname exactly.
[[env]]
key = "HEADSCALE_SERVER_URL"
value = "https://headscale.example.com"
```

A control server should never scale to zero or run two copies against one SQLite file,
hence `mode = "fixed"` with a single instance. The disk is `provider = "local"`, which
[Persistent Storage](/disks) recommends for SQLite — and headscale already opens the
database in WAL mode, so there's nothing to configure. Note that any disk pins the app to
the coordinator node.

## Deploy

<CliCommand context="client">
```bash
# Validate the config without building
miren deploy --analyze -a headscale

miren deploy -a headscale -f
```
</CliCommand>

## Add a hostname route

The route target must match `HEADSCALE_SERVER_URL` exactly — clients are handed that URL
and will use it for every subsequent request.

<CliCommand context="client">
```bash
miren route set headscale.example.com headscale
miren route list
```
</CliCommand>

## Raise the ingress timeout

This is the one server-side change headscale needs, and it's worth making before you connect
a client rather than after.

A Tailscale client holds a long-poll connection open to `/machine/map` and expects it to
stay open. Miren's ingress puts an idle read deadline on the connection to a backend, set by
`http_request_timeout` and **defaulting to 60 seconds**. The deadline resets whenever data
arrives, so the stream survives as long as headscale keeps talking — and headscale sends a
keepalive every **50 seconds plus up to 9 seconds of jitter**.

Those two defaults leave as little as one second of headroom, and the jitter means it varies
per session — the kind of margin that produces intermittent, hard-to-attribute drops rather
than a clean failure. Give it real room, in the server config file
(`/etc/miren/server.toml`, or `/var/lib/miren/config/server.toml`):

```toml
[server]
http_request_timeout = 120
```

Restart the server afterward. The value is an integer number of seconds, not a duration
string — see [Server Configuration](/server-config#server).

## Verify

<CliCommand context="client">
```bash
miren app status -a headscale   # Current Version + active
miren sandbox list              # one running sandbox, service "web"

curl -s https://headscale.example.com/health   # {"status":"pass"}
```
</CliCommand>

:::note[One warning in the logs is expected]
`miren logs` will show
`WRN listening without TLS but ServerURL does not start with http://`. That's correct here:
TLS terminates at Miren's ingress and headscale itself serves plain HTTP behind it, while
`server_url` is properly `https://`. Nothing is wrong.
:::

## Create a user and register a node

The headscale CLI talks to the running server over the unix socket in
`/var/run/headscale`, so these have to run **in the live sandbox**:

<CliCommand context="client">
```bash
SANDBOX=$(miren sandbox list --format json | jq -r '.[0].id')

miren sandbox exec "$SANDBOX" -- headscale users create alice
miren sandbox exec "$SANDBOX" -- headscale preauthkeys create --user 1 --expiration 24h
miren sandbox exec "$SANDBOX" -- headscale nodes list
```
</CliCommand>

Passing a command after `--` runs it as argv, with no shell in the way, which is why this
works against a distroless image.

:::warning[`miren app run` is not the tool here]
`miren app run` builds a fresh ephemeral sandbox, which on a shell-less image fails outright
with `failed to create ephemeral sandbox: sandbox failed to start, status: status.dead`.
Even where it does start, it's a *different* container with no headscale server in it, so
the CLI would have no socket to talk to. Admin commands have to reach the running instance
via `miren sandbox exec`.
:::

:::note[No interactive shell on this image]
`miren sandbox exec <id>` with no command tries to open `/bin/sh` and fails with
`stat /bin/sh: no such file or directory`. Named commands work; an interactive prompt
doesn't. See [Getting a shell](#getting-a-shell) if you want one.
:::

Then, on the machine joining the tailnet, point Tailscale at your server and use the
preauth key:

```bash
tailscale up --login-server=https://headscale.example.com --authkey=<key>
```

:::info[What wasn't exercised]
Everything else on this page was run as written against a Miren cluster. The client join
just above, and the embedded DERP relay further down, follow
[headscale's own documentation](https://headscale.net) and were not tested end to end.
:::

## Getting a shell {#getting-a-shell}

If you'd rather be able to poke around inside the container, build on a base that has a
shell instead. headscale's binary is statically linked, so it lifts out cleanly:

```dockerfile
FROM docker.io/headscale/headscale:0.29.3 AS upstream

FROM debian:bookworm-slim
# bookworm-slim ships no CA bundle, and headscale needs one for the DERP map.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY --from=upstream /ko-app/headscale /usr/local/bin/headscale
COPY config.yaml /etc/headscale/config.yaml
```

With a shell present you set the command explicitly, using `exec` so headscale still ends up
as PID 1:

```toml
[services.web]
command = "exec headscale serve"
port_timeout = "120s"
```

Everything else in the recipe is unchanged, and `miren sandbox exec <id>` now opens a prompt.
The tradeoff is a larger image and a base you're responsible for patching.


## Running the embedded DERP relay

Tailscale clients prefer direct peer-to-peer connections and fall back to a DERP relay when
they can't get one. The config above borrows Tailscale's public relays, which is the simpler
choice and costs you nothing to operate.

To relay through your own server instead, enable headscale's embedded DERP. It needs two
things from Miren: the relay itself is served over HTTPS on your existing hostname, and STUN
needs a UDP port exposed on the host. This is also the part of the recipe that exercises
Miren's [non-HTTP ports](/traffic-routing#non-http-services-tcpudp).

Add the STUN port alongside the HTTP one, and turn the relay on with more `HEADSCALE_*`
overrides:

```toml
# A second port on the same service; the HTTP one above stays as it is.
[[services.web.ports]]
port = 3478
name = "stun"
type = "udp"
node_port = 3478

[[env]]
key = "HEADSCALE_DERP_SERVER_ENABLED"
value = "true"
[[env]]
key = "HEADSCALE_DERP_SERVER_REGION_ID"
value = "999"
[[env]]
key = "HEADSCALE_DERP_SERVER_REGION_CODE"
value = "miren"
[[env]]
key = "HEADSCALE_DERP_SERVER_REGION_NAME"
value = "Miren embedded DERP"
[[env]]
key = "HEADSCALE_DERP_SERVER_STUN_LISTEN_ADDR"
value = "0.0.0.0:3478"
[[env]]
key = "HEADSCALE_DERP_SERVER_PRIVATE_KEY_PATH"
value = "/data/derp_server_private.key"
```

On boot headscale logs `stun server started at [::]:3478` and advertises a region whose node
carries `HostName: <your server_url host>`, `DERPPort: 443`, `STUNPort: 3478`. Open 3478/udp
in any cloud firewall in front of the cluster — node ports are not opened for you, and
[Firewall](/firewall) covers the inbound rules.

:::warning[Relayed traffic runs through the HTTP ingress]
The relay is advertised on port 443 at your hostname, so it rides the same ingress path as
everything else and inherits the `http_request_timeout` idle deadline described above. A
relayed session that goes quiet for longer than that window gets its connection torn down.
If you depend heavily on relaying, keep the public DERP servers in `derp.urls` as a fallback
rather than removing them.
:::

## Roadblock checklist

1. The official image is **distroless** — no shell. Leave `command` out of `app.toml` so the
   image's `ENTRYPOINT` + `CMD` run as argv; setting one sends it through `/bin/sh -c` and
   the sandbox dies at startup.
2. Give the image a `CMD ["serve"]`. Upstream sets an `ENTRYPOINT` but no `CMD`, so without
   it headscale starts with no subcommand.
3. Name the service **`web`**, or routing returns `error acquiring lease: app/headscale`.
4. Set `listen_addr` to `0.0.0.0:8080`, never `127.0.0.1`.
5. `HEADSCALE_SERVER_URL` must match the routed hostname exactly — clients are handed that
   URL and keep using it.
6. Include a `dns` block, or startup fails on
   `dns.nameservers.global must be set when dns.override_local_dns is true`.
7. Keep the `server_url` host out from under `dns.base_domain`, or you get
   `server_url cannot be part of base_domain in a way that could make the DERP and headscale server unreachable`.
   `headscale.example.com` with a base domain of `ts.example.com` is fine;
   `example.com` as the base domain is not.
8. Raise `http_request_timeout` to 120; the 60s default leaves as little as one second of
   margin against headscale's 50–59s keepalive.
9. Use `miren sandbox exec <id> -- <cmd>` for admin commands. `miren app run` can't start on
   a shell-less image, and `sandbox exec` with no command can't open one either.
10. `WRN listening without TLS but ServerURL does not start with http://` is expected.
11. For the embedded DERP, open 3478/udp in the cloud firewall as well as declaring the
    node port.

## Next steps

- [App Configuration](/app-configuration) — the full `app.toml` reference in context
- [Persistent Storage](/disks) — local vs. Miren disks
- [Traffic Routing](/traffic-routing) — routes, and TCP/UDP node ports
- [Running Miren on a Tailnet](/tailscale) — the reverse case: a cluster that lives on an
  overlay network
