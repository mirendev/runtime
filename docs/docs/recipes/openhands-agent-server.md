---
title: Run an OpenHands agent server
description: A worked example — run OpenHands' agent server as a self-contained Miren app that executes the coding agent in its own container (no Docker socket) behind an API-key-authenticated HTTP/WebSocket API.
keywords: [openhands, agent server, ai agent, coding agent, agent canvas, backend, api, example, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Run an OpenHands agent server

[OpenHands](https://docs.openhands.dev) is an AI coding agent. Its
[Agent Canvas](https://docs.openhands.dev/openhands/usage/agent-canvas/backends) architecture
splits into a **backend** (an *agent server* that runs the agent in a workspace) and a
**frontend** (any Agent Canvas client that connects to it). This recipe deploys the
**agent server** as a self-contained Miren app.

The end state: OpenHands' agent server running as one Miren `web` service on port 8000,
executing the agent **inside its own container** — no Docker socket, no sibling sandbox
container — behind an HTTP/WebSocket API protected by a session key, with conversation and
workspace state on a persistent disk. You point an Agent Canvas client (or the OpenHands SDK)
at its URL to drive it.

:::info[This is the backend, not the whole UI]
OpenHands' full local app spawns a **separate sandbox container per conversation via the host
Docker socket** — which Miren does not expose. The *agent server* is the piece that runs the
agent in-process in its own container, so it's the part that fits Miren cleanly. Run an
[Agent Canvas](https://docs.openhands.dev/openhands/usage/agent-canvas) client elsewhere (or in
the browser) and connect it to this server.
:::

## How it works

1. The Miren `web` service runs `openhands-agent-server` on `0.0.0.0:8000`.
2. It authenticates clients with a session key (`X-Session-API-Key` header).
3. A client (Agent Canvas or the SDK) creates a conversation, passing the LLM model + key and
   the agent's tools. The agent runs **in this container**, reading/writing its workspace on
   the mounted disk.
4. Conversations, the agent workspace, and bash-event history persist under `/workspace`.

## Prerequisites

- `miren` CLI installed and authenticated (`miren whoami`).
- Access to the target cluster and its org.
- An **LLM API key** (e.g. Anthropic) — supplied by the *client* when it starts a
  conversation, and encrypted at rest by the server's `OH_SECRET_KEY`.

## Select the target cluster

<CliCommand context="client">
```bash
# Add a cluster your cloud identity can see (interactive picker; pins the TLS fingerprint)
miren cluster add -i cloud

miren whoami -C openhands
```
</CliCommand>

The commands below target it with `-C openhands`. Omit `-C` to use your default cluster.

## Use the upstream image

OpenHands publishes a ready-to-run Python agent-server image. Miren can use its working
directory, entrypoint, and server defaults directly, so this recipe doesn't need a Dockerfile
or a shell command. The versioned tag keeps deploys reproducible. Bump it deliberately when
you want a newer OpenHands release.

## The app.toml

This file lives at `.miren/app.toml`.

```toml
name = "openhands-agent-server"

[services.web]
image = "ghcr.io/openhands/agent-server:1.44.1-python"
# The image exposes the API and noVNC ports, so select the API port explicitly.
port = 8000
port_type = "http"
port_timeout = "300s"

[services.web.concurrency]
mode = "fixed"
num_instances = 1

# Node-local persistence for conversations, the agent workspace, and bash-event history.
[[services.web.disks]]
name = "workspace"
provider = "local"
mount_path = "/workspace"

# CORS origin for a browser-based Agent Canvas client (set to your frontend's URL).
[[env]]
key = "OH_ALLOW_CORS_ORIGINS_0"
value = "https://your-agent-canvas-frontend.example.com"

[[env]]
key = "OH_SESSION_API_KEYS_0"
required = true
sensitive = true
[[env]]
key = "OH_SECRET_KEY"
required = true
sensitive = true
```

The server defaults to port 8000. Setting `OH_SESSION_API_KEYS_0` enables authentication and
also makes it listen on `0.0.0.0`, so no command-line override is needed.

:::warning[Use a `local` disk, and let it run as the image's user]
Mount the image's `/workspace` directory with `provider = "local"`. The server's default
conversation, project, and bash-event paths all live below that directory. The image runs as
its own `openhands` user, and Miren makes the disk writable for that user automatically.
:::

:::warning[The agent runs arbitrary shell in this container]
A client can make the agent execute shell commands against `/workspace`. The pinned image
grants the `openhands` user passwordless `sudo`, so those commands can become root inside the
container. Treat the container as the isolation boundary and the workspace as untrusted. Use
a dedicated, revocable session key and keep the server reachable only over its authenticated
API.
:::

Unlike a typical app, the agent server takes no `LLM_API_KEY` of its own — the client passes the
model and key when it creates a conversation, and `OH_SECRET_KEY` encrypts those secrets where
the server persists them.

## Deploy

Generate the two secrets once, save them somewhere safe, and pass them with `-s`:

<CliCommand context="client">
```bash
SESSION_KEY=$(openssl rand -hex 32)   # clients send this as X-Session-API-Key
SECRET_KEY=$(openssl rand -hex 32)    # encrypts stored secrets

miren deploy -a openhands-agent-server -C openhands -f \
  -s "OH_SESSION_API_KEYS_0=$SESSION_KEY" \
  -s "OH_SECRET_KEY=$SECRET_KEY"
```
</CliCommand>

Validate the config without building at any time:

<CliCommand context="client">
```bash
miren deploy --analyze -a openhands-agent-server -C openhands
```
</CliCommand>

## Add a hostname route

<CliCommand context="client">
```bash
miren route set agent.openhands.clusters.miren.run openhands-agent-server -C openhands
miren route list -C openhands
```
</CliCommand>

## Verify

<CliCommand context="client">
```bash
miren app status -a openhands-agent-server -C openhands   # Current Version + active
miren sandbox list -C openhands                           # one running sandbox, service "web"

# Health check is public; the API requires the session key:
curl -s -o /dev/null -w "%{http_code}\n" https://agent.openhands.clusters.miren.run/alive          # 200
curl -s -o /dev/null -w "%{http_code}\n" https://agent.openhands.clusters.miren.run/api/conversations   # 401
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "X-Session-API-Key: $SESSION_KEY" \
  https://agent.openhands.clusters.miren.run/api/conversations                                     # 200/422 (auth OK)
```
</CliCommand>

## Connect a client

Run an [Agent Canvas](https://docs.openhands.dev/openhands/usage/agent-canvas) frontend
(locally, or as its own service), open **Manage Backends → Add Backend**, and enter:

- **URL:** `https://agent.openhands.clusters.miren.run`
- **API key:** the **session key** you generated for `OH_SESSION_API_KEYS_0` at deploy time
  (the `$SESSION_KEY` from the deploy step). This is the same value clients send as the
  `X-Session-API-Key` header.

:::warning[Save the session key when you generate it]
`OH_SESSION_API_KEYS_0` is a `sensitive` env var, so Miren masks it in `miren app status` and
logs — it can't be read back after deploy. Store the value you generated somewhere safe (a
password manager); it's what you paste into the frontend. Lost it? Deploy again with a new
`-s "OH_SESSION_API_KEYS_0=..."` to rotate it, then update the frontend.
:::

For a browser-based frontend, set `OH_ALLOW_CORS_ORIGINS_0` (above) to that frontend's origin.
You can also drive the server directly with the OpenHands SDK, which sends the same
`X-Session-API-Key` header and passes your LLM config per conversation.

## Roadblock checklist

1. The image exposes both **8000** (API) and **8002** (noVNC), so select `port = 8000` explicitly.
2. Mount the disk at `/workspace` so the image's default state paths land on it.
3. Use a `provider = "local"` disk and keep the image's `openhands` user.
4. Clients authenticate with the `X-Session-API-Key` header; set `OH_SESSION_API_KEYS_0` and `OH_SECRET_KEY`.
5. For a browser frontend, set `OH_ALLOW_CORS_ORIGINS_0` to its origin.
6. The client — not the server — supplies the LLM model and key.
7. Raise `port_timeout`; the image is large and first boot is slow.

## Next steps

- [App Configuration](/app-configuration) — the full `app.toml` reference in context
- [Persistent Storage](/disks) — local vs. Miren disks
- [Traffic Routing](/traffic-routing) — how the `web` service and routes fit together
- [Deploy the Amp agent runner](/recipes/amp-runner) — another headless coding-agent recipe
