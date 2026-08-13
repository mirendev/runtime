---
title: Traffic Routing
description: Route HTTP traffic to apps via hostnames, and configure non-HTTP ports for TCP/UDP services.
keywords: [routing, routes, hostname, dns, ports, tcp, udp, load balancing, wildcard]
---

import CliCommand from '@site/src/components/CliCommand';

# Traffic Routing

Miren handles getting traffic to your app. For HTTP apps, this works automatically — deploy and add a route. For non-HTTP services like IRC servers, game servers, or custom TCP/UDP protocols, you configure ports explicitly.

## Minimum working example

Map a hostname to your app:

<CliCommand context="client">
```miren
miren route set myapp.example.com myapp
```
</CliCommand>

Requests to that hostname now reach your `web` service, with TLS provisioned automatically. For a non-HTTP service, expose a port directly in `.miren/app.toml`:

```toml
[[services.irc.ports]]
port = 6667
name = "irc"
type = "tcp"
node_port = 6667
```

## Web Traffic (HTTP)

When you deploy an app, the `web` service automatically receives HTTP traffic. No port configuration is needed — Miren handles TLS, routing, and load balancing.

### Adding a Route

Map a hostname to your app:

<CliCommand context="client">
```miren
miren route set myapp.example.com myapp
```
</CliCommand>

Requests to that hostname are forwarded to your `web` service. TLS certificates are provisioned automatically (see [TLS Certificates](/tls)).

### Wildcard Routes

Route all subdomains of a domain to a single app using a wildcard:

<CliCommand context="client">
```miren
miren route set '*.myapp.example.com' myapp
```
</CliCommand>

A wildcard route `*.myapp.example.com` matches any subdomain like `foo.myapp.example.com` or `bar.myapp.example.com`. It does **not** match the bare domain `myapp.example.com` — add a separate route for that if needed.

If an exact route also exists for a specific subdomain, the exact route takes priority. For example, if you have both `*.myapp.example.com` and `api.myapp.example.com`, requests to `api.myapp.example.com` use the exact route while all other subdomains use the wildcard.

The wildcard `*` must be the first label — patterns like `foo.*.example.com` are not supported. The domain must have at least two labels after the wildcard (e.g., `*.example.com` is valid, `*.com` is not).

For [Pull Request Environments](/pr-environments), you don't need a wildcard route — any subdomain of an existing route automatically resolves to its ephemeral label. You only need wildcard *DNS* pointing at your cluster.

### Custom Domains

To put your own domain in front of an app on Miren, point DNS at your cluster and set a route. TLS provisions automatically.

**Find your cluster's hostname.** Clusters registered with Miren Cloud get a hostname under `miren.systems` — something like `cluster-jwomf2l0tn8z.miren.systems`. You'll find yours on the cluster page in [miren.cloud](https://miren.cloud). If you're running standalone without cloud, use your server's public hostname or IP.

**Point DNS at your cluster.** For most setups, point both the apex and a wildcard so you can add or change routes later without touching DNS again:

```text
yourdomain.com.      ALIAS    cluster-jwomf2l0tn8z.miren.systems.
*.yourdomain.com.    CNAME    cluster-jwomf2l0tn8z.miren.systems.
```

Most DNS providers don't allow a CNAME at the apex (`yourdomain.com` with no subdomain). Use an ALIAS or ANAME record where your provider supports it, or an A record pointing at your cluster's IP. The wildcard record can always be a CNAME.

If you only need one hostname on Miren, a single CNAME for that name is fine on its own:

```text
app.yourdomain.com.  CNAME    cluster-jwomf2l0tn8z.miren.systems.
```

**Then set a route.** Map any host that resolves to your cluster to an app:

<CliCommand context="client">
```miren
miren route set app.yourdomain.com myapp
```
</CliCommand>

If you set up wildcard DNS above, you can also send every subdomain to the same app with a [wildcard route](#wildcard-routes):

<CliCommand context="client">
```miren
miren route set '*.yourdomain.com' myapp
```
</CliCommand>

You don't have to do anything else for HTTPS — Miren provisions Let's Encrypt certificates as requests arrive. See [TLS Certificates](/tls) for the details.

### Request Timeouts

Miren gives up on a proxied request once it has gone quiet for 60 seconds — nothing from your app, nothing to send. That covers most apps, but it cuts off patterns that are silent by design: long polling, an endpoint that waits on a slow job before writing its first byte, or a report that takes minutes to assemble.

Raise the limit for just that route:

<CliCommand context="client">
```miren
miren route timeout longpoll.yourdomain.com 10m
```
</CliCommand>

Check what a route is using, and put it back on the cluster default:

<CliCommand context="client">
```miren
miren route timeout longpoll.yourdomain.com
miren route timeout longpoll.yourdomain.com --clear
```
</CliCommand>

The value is a duration string such as `10m`, `300s`, or `2h`, and must be positive. `miren route list` shows a `TIMEOUT` column with each route's override, or `-` when it uses the default.

The 60-second default itself is the `server.http_request_timeout` setting — see [Server Configuration](/server-config). A per-route value overrides it for that route only.

:::note[Streaming responses are already fine]
The timeout measures silence, not total duration. A server-sent-events stream or a WebSocket that keeps sending data will run indefinitely on the default — you only need an override when the connection genuinely goes quiet for longer than a minute.
:::

### Choosing a Port

Miren sets the `PORT` environment variable to tell your app which port to listen on. Your app should bind to `PORT`:

```toml
[services.web]
command = "node server.js"
# App reads process.env.PORT and listens there
```

To choose a specific port, set it in `.miren/app.toml`:

```toml
[services.web]
command = "gunicorn app:app --bind 0.0.0.0:8000"
port = 8000
```

Miren sets `PORT=8000` and routes traffic there.

:::tip[Binding the wrong port]
The most common deploy snag when porting an existing app is a hardcoded port that ignores `$PORT`. If your app listens on, say, `:8080` while Miren expects `:3000`, Miren now notices the mismatch: when the configured port never comes up but the app is clearly listening somewhere else, it routes to the port the app actually bound and logs a note on the sandbox (`miren logs sandbox <id>`) telling you which port that was. It's a safety net, not a substitute for config — set `port` (or read `$PORT`) so the expected and actual ports line up.

Two things still trip this up. If your app binds to `127.0.0.1` instead of `0.0.0.0`, Miren can't reach it from outside the container no matter the port, so bind to all interfaces. And if the app opens several ports and none of them is the configured one, Miren won't guess which to route to — it fails with a message naming the candidates so you can pick the right one in `app.toml`.
:::

## Non-HTTP Services (TCP/UDP)

To expose services that don't speak HTTP — an IRC server, a game server, a custom protocol — use the `ports` array in `.miren/app.toml` with a `node_port` to make the port reachable from outside the cluster.

```toml
[services.irc]
command = "./ircd"

[[services.irc.ports]]
port = 6667
name = "irc"
type = "tcp"
node_port = 6667

[[services.irc.ports]]
port = 6697
name = "irc-tls"
type = "tcp"
node_port = 6697
```

With this config:
- The IRC server listens on ports 6667 and 6697 inside its container
- Clients connect to those same ports on your Miren host
- Miren forwards the traffic and load-balances across instances

### Port Configuration

Each entry in `ports` accepts:

| Field | Required | Description | Default |
|-------|----------|-------------|---------|
| `port` | Yes | Port your process listens on (1–65535) | — |
| `name` | Yes | A unique name for this port | — |
| `type` | No | `"http"` for web traffic, `"tcp"` for raw TCP, `"udp"` for UDP | `"http"` |
| `node_port` | No | Port to expose on the host machine (1–65535) | (none) |

### Node Ports

The `node_port` is what makes a non-HTTP service reachable from outside. Without it, the port is only accessible to other services within the app.

Node port constraints:
- Must be unique across all apps on the cluster — Miren checks for conflicts at deploy time
- Cannot overlap with ports Miren uses (80, 443, 8443)

If your cloud provider uses security groups or network ACLs, you'll need to allow traffic on your node ports (see [Firewall Configuration](/firewall)).

## Multiple Ports per Service

A single service can expose a mix of HTTP and non-HTTP ports. This is useful when a service needs an HTTP endpoint for health checks or an API alongside a raw protocol port:

```toml
[services.app]
command = "./server"

# HTTP endpoint for health checks and API
[[services.app.ports]]
port = 3000
name = "http"
type = "http"

# TCP port for the data protocol
[[services.app.ports]]
port = 7000
name = "data"
type = "tcp"
node_port = 7000
```

The HTTP port is routed through Miren's web traffic pipeline (routes, TLS, autoscaling). The TCP port is exposed directly via the node port. Both reach the same running process.

### Same Port, Different Protocols

Some protocols need both TCP and UDP on the same port number. Declare them as separate entries:

```toml
[[services.dns.ports]]
port = 53
name = "dns-udp"
type = "udp"
node_port = 53

[[services.dns.ports]]
port = 53
name = "dns-tcp"
type = "tcp"
node_port = 5353
```

Port numbers can repeat as long as each port/type pair is unique within the service (since `"tcp"` and `"udp"` use different transport protocols).

### The `PORT` Environment Variable

When using the `ports` array, Miren sets `PORT` to:
1. The first port with `type = "http"`, if one exists
2. Otherwise, the first port in the array

:::warning[PORT is managed by Miren]
`PORT` is managed by Miren and can't be overridden.
:::

## Service-to-Service Communication

Services within the same app can talk to each other using internal DNS. Each service is reachable at `<service>.app.miren`:

```toml
name = "myapp"

[[env]]
key = "DATABASE_URL"
value = "postgres://user:pass@postgres.app.miren:5432/mydb"

[services.web]
command = "node server.js"

[services.postgres]
image = "postgres:16"

[services.postgres.concurrency]
mode = "fixed"
num_instances = 1
```

Internal traffic goes directly between containers — it doesn't pass through Miren's routing layer. Services connect on their standard ports (5432 for PostgreSQL, 6379 for Redis) without any Miren port configuration needed.

:::note[Internal DNS scope]
Internal DNS only works between services in the same app. Cross-app communication is not currently supported.
:::

## Simple Port Configuration

If your service only has one port, you can use the simpler single-field syntax instead of the `ports` array:

```toml
[services.web]
port = 8000
port_name = "http"
port_type = "http"
```

| Field | Description | Default |
|-------|-------------|---------|
| `port` | Port your process listens on | Set by `PORT` env var |
| `port_name` | Name for the port | Service name |
| `port_type` | `"http"` or `"tcp"` | `"http"` |

:::warning[Port field restrictions]
You can't mix these fields with the `ports` array on the same service.
:::

## How It Works Under the Hood

:::info[For the curious]
You don't need to understand these details to use Miren. This section is for the curious.
:::

**HTTP traffic** flows through Miren's HTTP ingress — a reverse proxy that listens on ports 80 and 443. When a request arrives, the ingress looks up the hostname to find the matching app, ensures a sandbox is running, and proxies the request to the sandbox's HTTP port.

**Non-HTTP traffic** is routed by the kernel using nftables NAT rules. When you deploy a service with non-HTTP ports, Miren allocates an internal IP for the service and programs firewall rules that forward matching traffic (by port and protocol) to the running sandboxes. As instances scale up and down, these rules are updated automatically to load-balance across available endpoints.

## Examples

### IRC Server

```toml
name = "irc"

[services.irc]
command = "./ircd"

[[services.irc.ports]]
port = 6667
name = "irc"
type = "tcp"
node_port = 6667

[[services.irc.ports]]
port = 6697
name = "irc-tls"
type = "tcp"
node_port = 6697
```

### Game Server with HTTP Admin Panel

```toml
name = "gameserver"

[services.game]
command = "./server"

[[services.game.ports]]
port = 3000
name = "admin"
type = "http"

[[services.game.ports]]
port = 27015
name = "game"
type = "udp"
node_port = 27015

[services.game.concurrency]
mode = "fixed"
num_instances = 1
```

### TCP Echo Server

```toml
name = "tcp-echo"

[services.echo]
command = "./tcp-echo"

[[services.echo.ports]]
port = 3000
name = "health"
type = "http"

[[services.echo.ports]]
port = 7000
name = "echo"
type = "tcp"
node_port = 7000

[services.echo.concurrency]
mode = "fixed"
num_instances = 1
```

## Next Steps

- [app.toml Reference](/app-toml#ports) — Complete field reference for port configuration
- [Services](/services) — Defining services, commands, images, and scaling
- [TLS Certificates](/tls) — How HTTPS works for HTTP services
- [Firewall Configuration](/firewall) — Host-level firewall rules and cloud provider setup
- [Application Scaling](/scaling) — How services scale up and down
