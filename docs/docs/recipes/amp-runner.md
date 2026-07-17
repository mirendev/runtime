---
title: Run an Amp agent on Miren
description: A worked example — run the Amp coding agent as a headless runner on Miren that dials out to ampcode.com and shows up as a Machine you drive from the web app.
keywords: [amp, ampcode, ai agent, runner, machine, headless, remote thread, example, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Run an Amp agent on Miren

Run [Amp](https://ampcode.com), a coding agent, as a headless **runner** on a
Miren cluster. The container dials *out* to ampcode.com, registers as a Machine, and then
waits. You create threads from the ampcode.com web app, pick this runner, and the work
executes on Miren — streaming back to your browser.

The end state: a single portless Miren service running `amp --no-tui`, connected to
ampcode.com with an API key, with its working directory on a persistent Miren disk. There is
**no inbound port, no dashboard, and no HTTP ingress** — ampcode.com is the control plane and
the runner only makes outbound connections.

Along the way it exercises real Miren behavior — custom Dockerfile builds, a **portless
background service**, `0.0.0.0`-free networking (nothing listens), a writable network disk,
and single-instance concurrency — so it doubles as a tour of running an outbound-only worker.

:::info[This is an application recipe, not a language guide]
For getting your own source code onto Miren, start with [Deployment](/deployment) and the
[Language Guides](/guides). This page is about running a third-party agent as an
outbound-only service.
:::

## How it works

1. The Miren service runs `amp --no-tui --runner-id <id>` as a long-lived process.
2. On start it reads `AMP_API_KEY` and dials ampcode.com, registering as runner `<id>`.
3. Its `settings.json` has `amp.remoteThreadCreation.enabled: true`, so it accepts threads
   created remotely.
4. From the ampcode.com web app you create a thread on that runner. It runs in the runner's
   working directory (`/workspace` on the disk) and streams output back to ampcode.com — and
   to `miren logs`.

Because a runner is identified by **host + working directory**, the persistent disk keeps its
workspace (cloned repos, build caches) stable across redeploys.

## Prerequisites

- `miren` CLI installed and authenticated (`miren whoami`).
- Access to the target cluster and its org.
- An **Amp API key** from [ampcode.com/settings](https://ampcode.com/settings).
- Outbound HTTPS to ampcode.com allowed from the cluster (see [Firewall](/firewall) if you
  restrict egress).

## Select the target cluster

If the cluster isn't configured locally yet, list what your identity can see and add it
(this pins the TLS fingerprint):

<CliCommand context="client">

```bash
# Add a cluster your cloud identity can see (interactive picker; pins the TLS fingerprint)
miren cluster add -i cloud

miren whoami -C amp
```

</CliCommand>

The commands below target it explicitly with `-C amp`. Omit `-C` to use your default cluster.

## The Dockerfile

Amp ships as a CLI, not a container image, so build a thin image that installs it with the
official installer (a standalone binary — no Node required). Include the tools the agent will
reach for when it runs shell commands (`git`, `ripgrep`, `curl`):

```dockerfile
FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends git ca-certificates ripgrep curl \
  && rm -rf /var/lib/apt/lists/*

# Install the Amp CLI to a fixed location and put it on PATH. AMP_VERSION pins the
# release for reproducible builds; drop it to track latest, or bump it deliberately.
ENV AMP_HOME=/opt/amp
RUN curl -fsSL https://ampcode.com/install.sh | AMP_VERSION=0.0.1784319456-g6a2cfc bash \
  && ln -s /opt/amp/bin/amp /usr/local/bin/amp

COPY runner.sh /usr/local/bin/runner.sh
RUN chmod +x /usr/local/bin/runner.sh
```

:::note[Why the extra symlink]
The installer drops the binary at `$AMP_HOME/bin/amp` and tries to symlink it into a
per-user PATH directory (`~/.local/bin`). Building as root, that isn't reliably on `PATH`, so
the recipe symlinks `/opt/amp/bin/amp` into `/usr/local/bin` explicitly.
:::

:::note[No ENTRYPOINT or CMD needed]
Miren runs the service `command` from `app.toml` via `/bin/sh -c "<cmd>"`, so this image
doesn't need an `ENTRYPOINT` or `CMD` — `runner.sh` is invoked directly (see below).
:::

## The runner entrypoint

`runner.sh` prepares a writable home and settings on the disk, enables remote thread
creation, clears any stale runner lock left on the disk by a prior deploy, then hands off to
the runner with `exec` so signals reach Amp and Miren can stop it cleanly:

```bash
#!/usr/bin/env bash
set -euo pipefail

WORKSPACE="${AMP_WORKSPACE:-/workspace}"
mkdir -p "$HOME" "$WORKSPACE" "$(dirname "$AMP_SETTINGS_FILE")"

# Enable remote thread creation so ampcode.com can dispatch threads to this runner.
if [ ! -f "$AMP_SETTINGS_FILE" ]; then
  cat > "$AMP_SETTINGS_FILE" <<'JSON'
{
  "amp.remoteThreadCreation.enabled": true,
  "amp.notifications.enabled": false
}
JSON
fi

# Amp records a per-directory runner lock (a pidfile under its cache). With HOME on a
# persistent disk that survives redeploys, a stale pidfile makes the new runner refuse to
# start ("Another Amp process is already serving remote threads"). The Miren disk lease
# guarantees only one runner container at a time, so any pidfile here at boot is stale.
rm -f "$HOME/.cache/amp/pids/"*.pid 2>/dev/null || true

# Optional: seed a repo for the agent to work in. Adapt or remove.
# if [ ! -d "$WORKSPACE/repo/.git" ]; then
#   git clone https://github.com/you/your-repo "$WORKSPACE/repo"
# fi

cd "$WORKSPACE"
exec amp --no-tui --runner-id "$AMP_RUNNER_ID"
```

## The app.toml

This file lives at `.miren/app.toml` in your project (not the repo root). If you haven't set
the project up yet, `miren init` creates it for you; otherwise create the `.miren/` directory
and add the file below. The `Dockerfile` and `runner.sh` stay at the project root.

```toml
name = "amp-runner"

[build]
dockerfile = "Dockerfile"

# Portless background service: nothing listens, so no `port`/`port_type`.
[services.runner]
command = "runner.sh"

[services.runner.concurrency]
mode = "fixed"
num_instances = 1

[[services.runner.disks]]
name = "workspace"
provider = "miren"
mount_path = "/workspace"
size_gb = 20
filesystem = "ext4"

[[env]]
key = "AMP_RUNNER_ID"
value = "miren-amp"  # a valid hostname; shown as the Machine name on ampcode.com
[[env]]
key = "AMP_SETTINGS_FILE"
value = "/workspace/amp/settings.json"
[[env]]
key = "HOME"
value = "/workspace/home"
[[env]]
key = "AMP_LOG_LEVEL"
value = "info"

[[env]]
key = "AMP_API_KEY"
required = true
sensitive = true
```

:::warning[Enable remote thread creation or nothing runs]
A runner with `amp.remoteThreadCreation.enabled` unset connects to ampcode.com but silently
accepts no threads. `runner.sh` writes it into the settings file pointed at by
`AMP_SETTINGS_FILE` — keep that in place.
:::

:::warning[Portless service — name it `runner`, not `web`]
This service has no `port`/`port_type`, so Miren does no HTTP ingress and no port health
check. Do **not** name it `web`: Miren's ingress routes an app's hostname to the `web`
service, and a portless `web` service returns `error acquiring lease: app/amp-runner`
(HTTP 500) for every request. Any other name (like `runner`) is fine.
:::

:::warning[Keep the process alive — it self-reconnects]
`amp --no-tui` is a long-lived daemon (unlike `amp -x`, which runs one prompt and exits).
Miren restarts the service if the process exits, and the runner re-registers with ampcode.com
after a bounce. On redeploy it may briefly appear as a new Machine connection before the old
one drops.
:::

:::warning[Stable runner id and working directory]
A runner is identified by host **and** working directory. Set a stable `AMP_RUNNER_ID` (a
valid hostname) so the Machine is easy to pick in the web app, and keep the working directory
fixed at `/workspace`. The directory does not have to be a git repo.
:::

:::danger[Constrain the agent — it runs arbitrary shell]
A runner executes agent-driven shell commands as the container user with full access to
`/workspace`. Use a dedicated Amp API key (revocable independently), treat the workspace as
untrusted, and consider Amp's `amp.commands.allowlist` / `amp.commands.strict` settings to
limit what it can run unattended.
:::

:::warning[A Miren disk forces single-instance, churny rollouts]
A `provider = "miren"` (network) disk holds one exclusive lease, so it requires
`concurrency mode = "fixed"` with `num_instances = 1`. On redeploy the new instance can't
mount `/workspace` until the old one releases the lease, so `miren deploy` may print
`did not become healthy` while the instance actually comes up a few seconds later — re-check
`miren app status` / `miren sandbox list` before assuming failure. If you'd rather have snappy
rollouts and don't need node-independent storage, use `provider = "local"` (no lease, no
fixed-instance requirement; workspace is node-local).
:::

:::note[Point HOME and settings at the disk]
Amp reads `~/.config/amp` and may write cache and logs. Setting `HOME=/workspace/home` and
`AMP_SETTINGS_FILE=/workspace/amp/settings.json` keeps that state on the writable mounted
disk instead of a read-only image path.
:::

:::warning[Redeploys leave a stale runner lock on the disk]
Amp writes a per-directory runner lock — a pidfile under `$HOME/.cache/amp/pids/`. Because
`HOME` is on the persistent disk, that pidfile survives a redeploy, and the fresh runner
refuses to start with `Another Amp process is already serving remote threads for /workspace`.
`runner.sh` deletes stale pidfiles on boot before launching Amp; the disk's single-instance
lease guarantees no live runner is ever using them.
:::

Add a `.dockerignore` so local secrets and Miren state stay out of the build context (secrets
are passed with `miren deploy -s`, never baked into the image):

```text
.env
.miren
```

## Deploy

Non-secret config lives in `app.toml`. Pass the API key with `-s` (masked in output, stored
server-side); never bake it into the image:

<CliCommand context="client">
```bash
miren deploy -a amp-runner -C amp -f \
  -s "AMP_API_KEY=sgamp_..."
```
</CliCommand>

Validate the config without building at any time:

<CliCommand context="client">
```bash
miren deploy --analyze -a amp-runner -C amp
```
</CliCommand>

## Verify

<CliCommand context="client">
```bash
miren app status -a amp-runner -C amp       # Current Version + active
miren sandbox list -C amp                   # one running sandbox, service "runner"
miren logs -a amp-runner -C amp --since 5m  # look for the runner connecting to ampcode.com
```
</CliCommand>

Then open [ampcode.com](https://ampcode.com): the Machine `miren-amp` should appear as a
connected runner. Create a thread on it, send a prompt, and confirm the work executes — output
streams in the ampcode.com web UI and in `miren logs`.

## Using the runner

- **Create work from ampcode.com:** start a thread targeting the `miren-amp` Machine. It runs
  in `/workspace` on the cluster.
- **Pre-seed a repo:** clone into the workspace once, and it persists on the disk:

<CliCommand context="client">
```bash
miren sandbox list -C amp                   # grab the sandbox id
miren sandbox exec <sandbox-id> -C amp -- \
  git clone https://github.com/you/your-repo /workspace/repo
```
</CliCommand>

- **Rotate the key:** deploy again with a new `-s "AMP_API_KEY=..."`; the runner reconnects on
  restart.

## Roadblock checklist

1. Set `amp.remoteThreadCreation.enabled: true` (in `runner.sh`), or the runner accepts no threads.
2. Portless service: omit `port`/`port_type`, and name it `runner`, not `web` (else `error acquiring lease`).
3. Run the daemon `amp --no-tui`, not `amp -x` — the service command must not exit.
4. Give it a stable `AMP_RUNNER_ID` (valid hostname) and a fixed working directory.
5. A `miren` disk means `fixed` / `num_instances = 1`; expect `did not become healthy` noise on redeploy (verify state before retrying). Use `local` for snappy rollouts.
6. Point `HOME` and `AMP_SETTINGS_FILE` at the writable disk.
7. Clear Amp's stale runner pidfile on boot (in `runner.sh`), or redeploys crash-loop with `Another Amp process is already serving remote threads`.
8. The agent runs arbitrary shell — use a dedicated key and constrain it.

## Next steps

- [App Configuration](/app-configuration) — the full `app.toml` reference in context
- [Persistent Storage](/disks) — Miren disks vs. local disks
- [Deployment](/deployment) — deploying your own source to Miren
- [Firewall](/firewall) — controlling egress if you restrict outbound traffic
- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — building from your own Dockerfile
