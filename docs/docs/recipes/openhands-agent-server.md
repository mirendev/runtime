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
4. Conversations, the agent workspace, and bash-event history persist under `/data`.

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

## The Dockerfile

The agent server ships as a prebuilt image; wrap it and clear the entrypoint so Miren runs the
service command from `app.toml`:

```dockerfile
FROM ghcr.io/openhands/agent-server:main-python
ENTRYPOINT []
```

:::tip[Pin a version]
`main-python` tracks the latest `main` build of the Python agent-server variant. For
reproducible deploys, pin one of the commit-tagged images (e.g.
`ghcr.io/openhands/agent-server:<sha>-python`) instead and bump it deliberately.
:::

## The app.toml

This file lives at `.miren/app.toml`.

```toml
name = "openhands-agent-server"

[build]
dockerfile = "Dockerfile"

[services.web]
command = "exec /usr/local/bin/openhands-agent-server --host 0.0.0.0 --port 8000"
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
mount_path = "/data"

# The server's default state paths are RELATIVE to its working directory and fail to create;
# point them at absolute paths on the writable disk.
[[env]]
key = "OH_CONVERSATIONS_PATH"
value = "/data/conversations"
[[env]]
key = "OH_WORKSPACE_PATH"
value = "/data/project"
[[env]]
key = "OH_BASH_EVENTS_DIR"
value = "/data/bash_events"

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

:::warning[Set absolute workspace paths]
The server's `conversations_path` / `workspace_path` / `bash_events_dir` default to paths
**relative to the process working directory** (`workspace/…`), which it can't create at
startup — the server exits with `PermissionError: 'workspace'`. Set `OH_CONVERSATIONS_PATH`,
`OH_WORKSPACE_PATH`, and `OH_BASH_EVENTS_DIR` to absolute paths on the mounted disk.
:::

:::warning[Use a `local` disk, and let it run as the image's user]
Mount the workspace with `provider = "local"`. Don't add a `USER root` to the Dockerfile — the
image runs as its own `openhands` user, and a writable disk is made writable for the run user
automatically; forcing root opts out of that.
:::

:::warning[The agent runs arbitrary shell in this container]
A client can make the agent execute shell commands as the container user against `/data`. Use
a dedicated, revocable session key, treat the workspace as untrusted, and keep the server
reachable only over its authenticated API.
:::

Unlike a typical app, the agent server takes no `LLM_API_KEY` of its own — the client passes the
model and key when it creates a conversation, and `OH_SECRET_KEY` encrypts those secrets where
the server persists them.

Add a `.dockerignore` so local secrets stay out of the build context:

```text
.env
.miren
```

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

1. The agent server runs on **port 8000**, not 3000 — set `port = 8000`.
2. Set `OH_CONVERSATIONS_PATH` / `OH_WORKSPACE_PATH` / `OH_BASH_EVENTS_DIR` to absolute paths, or the server exits with `PermissionError: 'workspace'`.
3. Use a `provider = "local"` disk; don't force `USER root` (it opts out of the disk being made writable for the run user).
4. Clients authenticate with the `X-Session-API-Key` header; set `OH_SESSION_API_KEYS_0` and `OH_SECRET_KEY`.
5. For a browser frontend, set `OH_ALLOW_CORS_ORIGINS_0` to its origin.
6. The client — not the server — supplies the LLM model and key.
7. Raise `port_timeout`; the image is large and first boot is slow.

## Next steps

- [App Configuration](/app-configuration) — the full `app.toml` reference in context
- [Persistent Storage](/disks) — local vs. Miren disks
- [Traffic Routing](/traffic-routing) — how the `web` service and routes fit together
- [Deploy the Amp agent runner](/recipes/amp-runner) — another headless coding-agent recipe
