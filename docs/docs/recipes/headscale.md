---
title: Run a headscale control server
description: Self-host headscale, the open-source Tailscale control server, on Miren — reachable at your own hostname, with its database and keys on a persistent disk.
keywords: [headscale, tailscale, tailnet, control server, vpn, wireguard, derp, self-hosted, example, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Run a headscale control server

[headscale](https://headscale.net) is an open-source implementation of the Tailscale
control server — the coordination plane your Tailscale clients log in to, exchange keys
through, and get their network map from. Self-hosting it means your tailnet's coordination
belongs to you.

By the end you'll have headscale answering over HTTPS at a hostname you own, its database
and keys on a disk that survives redeploys, and a first node joined to your tailnet.

:::info[This is an application recipe, not a language guide]
For getting your own source code onto Miren, start with [Deployment](/deployment) and the
[Language Guides](/guides). This page is about self-hosting a prebuilt third-party server.
For the opposite topic — running a Miren **cluster** on a tailnet — see
[Running Miren on a Tailnet](/tailscale).
:::

## Prerequisites

- `miren` CLI installed and authenticated (`miren whoami`).
- Access to the target cluster and its org.
- A hostname you control, pointed at the cluster — see
  [Custom Domains](/traffic-routing#custom-domains) or claim one through
  [Miren Cloud subdomains](/miren-cloud/subdomains).
- **The ability to edit the cluster's server config.** Tailscale clients hold a connection
  to the control server open far longer than the ingress allows by default, so this recipe
  needs one cluster-wide setting changed before a client will stay connected. Details in
  [Give clients a longer timeout](#longer-timeout).

:::warning[That timeout is cluster-wide]
`http_request_timeout` applies to every app on the cluster, not just headscale. If you can't
change it — someone else runs the cluster, or other apps depend on the current value — nodes
will keep dropping their connection to headscale, and there's no way to work around it from
headscale's side.
:::

## Select the target cluster

<CliCommand context="client">

```bash
# Add a cluster your cloud identity can see (interactive picker; pins the TLS fingerprint)
miren cluster add

miren whoami
```

</CliCommand>

## The Dockerfile

You only need to add your config file and tell the image what to run:

```dockerfile
FROM docker.io/headscale/headscale:0.29.3

COPY config.yaml /etc/headscale/config.yaml

# The image sets an ENTRYPOINT but no CMD, so give it one.
CMD ["serve"]
```

That's the whole build. The upstream image already carries the CA bundle headscale needs to
fetch the DERP map, so there's nothing to install.

:::warning[Don't set a `command` for this app]
The headscale image ships without a shell, and a service that sets `command` needs one.
Leave it out — as the `app.toml` below does — and the image runs what its own `CMD` says.
This also means you can't open an interactive shell in the container; see
[Getting a shell](#getting-a-shell) if you want that.
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

# Bind on all interfaces. A 127.0.0.1 listener is unreachable from outside the
# container, so the app never comes up healthy.
listen_addr: 0.0.0.0:8080
metrics_listen_addr: 0.0.0.0:9090

# State on the mounted disk. headscale creates both on first start.
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

Any of these can move into `app.toml` as an environment variable instead: headscale accepts
`HEADSCALE_*` overrides for nested keys too, so `database.sqlite.path` becomes
`HEADSCALE_DATABASE_SQLITE_PATH`.

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

# Must be named `web` — that's the service a hostname route points at.
# No `command` here, on purpose: see above.
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

A control server shouldn't scale to zero or run two copies against one SQLite file, hence a
single fixed instance. [Persistent Storage](/disks) recommends a local disk for SQLite,
which is what this uses; note that any disk pins the app to the coordinator node.

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

## Give clients a longer timeout {#longer-timeout}

Do this before you connect a client. Tailscale clients keep a connection to the control
server open and idle for long stretches, longer than the ingress tolerates by default, and
headscale offers no setting to make them chattier. Left alone, nodes drop and reconnect for
no visible reason.

Raise the limit in the server config file (`/etc/miren/server.toml`, or
`/var/lib/miren/config/server.toml`) and restart the server:

```toml
[server]
http_request_timeout = 120
```

The value is seconds. See [Server Configuration](/server-config#server).

## Verify

<CliCommand context="client">
```bash
miren app status -a headscale   # Current Version + active
miren sandbox list              # a running sandbox for headscale, service "web"

curl -fsS https://headscale.example.com/health   # {"status":"pass"}
```
</CliCommand>

:::note[One warning in the logs is expected]
`miren logs` will show
`WRN listening without TLS but ServerURL does not start with http://`. That's correct here:
TLS terminates at Miren's ingress and headscale itself serves plain HTTP behind it, while
`server_url` is properly `https://`. Nothing is wrong.
:::

## Create a user and register a node

The `headscale` command only talks to its own running server, so these have to run inside
the live container. Find the sandbox in `miren sandbox list` — the one whose app is
`headscale` and service is `web` — and use its ID:

<CliCommand context="client">
```bash
miren sandbox list                              # find the headscale web sandbox

miren sandbox exec <id> -- headscale users create alice
miren sandbox exec <id> -- headscale users list   # note alice's numeric ID
miren sandbox exec <id> -- headscale preauthkeys create --user <user-id> --expiration 24h
miren sandbox exec <id> -- headscale nodes list
```
</CliCommand>

`preauthkeys create` takes the user's numeric ID, not the name, which is why you look it up
first. On a cluster running other apps, `miren sandbox list` will show their sandboxes too —
match on the app name rather than taking the first row.

:::warning[`miren app run` won't work here]
It starts a fresh, separate container rather than reaching the one serving traffic, so the
`headscale` command inside it has no server to talk to. Use `miren sandbox exec`.
:::

Then, on the machine joining the tailnet, point Tailscale at your server and use the preauth
key:

```bash
tailscale up --login-server=https://headscale.example.com --authkey=<key>
```

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

Now that there's a shell, set the command explicitly:

```toml
[services.web]
command = "exec headscale serve"
port_timeout = "120s"
```

Everything else in the recipe is unchanged, and `miren sandbox exec <id>` opens a prompt.
The tradeoff is a larger image and a base you're responsible for patching.

## Running the embedded DERP relay

Tailscale clients prefer direct peer-to-peer connections and fall back to a DERP relay when
they can't get one. The config above borrows Tailscale's public relays, which is the simpler
choice and costs you nothing to operate.

To relay through your own server instead, enable headscale's embedded DERP. The relay is
served over HTTPS on your existing hostname; STUN additionally needs a UDP port open on the
host, declared as a [node port](/traffic-routing#non-http-services-tcpudp).

Add the STUN port alongside the HTTP one, and turn the relay on:

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

On boot headscale logs `stun server started at [::]:3478` and advertises the new region.
Open 3478/udp in any cloud firewall in front of the cluster — node ports aren't opened for
you, and [Firewall](/firewall) covers the inbound rules.

:::warning[Keep the public relays as a fallback]
Relayed traffic reaches your hostname over the same HTTPS path as everything else, so a
relayed session that goes quiet long enough hits the timeout from
[earlier](#longer-timeout). Leave Tailscale's servers in `derp.urls` alongside your own
rather than removing them.
:::

## Next steps

- [App Configuration](/app-configuration) — the full `app.toml` reference in context
- [Persistent Storage](/disks) — local vs. Miren disks
- [Traffic Routing](/traffic-routing) — routes, and TCP/UDP node ports
- [Running Miren on a Tailnet](/tailscale) — the reverse case: a cluster that lives on an
  overlay network
