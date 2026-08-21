---
title: Troubleshooting
description: Step-by-step guide to diagnosing issues with Miren applications, builds, networking, and server health.
keywords: [troubleshooting, debugging, errors, miren doctor, health check]
---

import CliCommand from '@site/src/components/CliCommand';

# Troubleshooting

A step-by-step guide to diagnosing issues with your Miren applications and server.

## Quick health check

Start with `miren doctor` to verify your environment is set up correctly:

<CliCommand context="client">
```miren
miren doctor
```
</CliCommand>

This checks your configuration, server connectivity, and authentication. It provides context-aware suggestions when it detects issues. You can also run the subcommands individually:

<CliCommand context="client">
```miren
miren doctor config   # Check cluster configuration
miren doctor server   # Check server connectivity
miren doctor auth     # Check authentication
```
</CliCommand>

:::tip[Get a combined diagnosis]
Install the [Miren agent skills](/agent-skills) and ask your AI coding agent to
"check the health of this app" or "check the health of this cluster."
The `app-health` and `cluster-health` skills combine status, deployment history,
logs, and diagnostics into a prioritized report with suggested next steps.
:::

## App not starting

If your app is deployed but not responding:

**1. Check the app status**

<CliCommand context="client">
```miren
miren app status -a myapp
```
</CliCommand>

This shows the current deployment state, version, configuration, and any error messages.

**2. Check the logs**

<CliCommand context="client">
```miren
# Recent logs
miren logs -a myapp

# Live tail
miren logs -a myapp -f

# Filter for errors
miren logs -a myapp -g error

# Logs from a specific service
miren logs -a myapp --service web
```
</CliCommand>

**3. Check sandbox state**

<CliCommand context="client">
```miren
miren sandbox list
```
</CliCommand>

Look for sandboxes that are stuck in `pending` or `not_ready`, or that have gone `dead`. Use `--all` to include dead sandboxes in the output.

## Deploy failed

**1. Find the failed deployment**

<CliCommand context="client">
```miren
miren app history -a myapp
```
</CliCommand>

Failed deployments are marked with a red `✗`. Use `--detailed` for more info including error messages and git SHAs.

**2. Check build logs**

<CliCommand context="client">
```miren
miren logs build -a myapp VERSION
```
</CliCommand>

Replace `VERSION` with the version from the deployment history. This shows the build output so you can see where things went wrong. See [Logs](/logs) for more on filtering and following logs.

## Server-level issues

If you suspect the Miren server itself is having problems:

**1. Check server logs on the host**

For systemd installations:

<CliCommand context="server">
```bash
sudo journalctl -u miren -f
```
</CliCommand>

For container installations (Docker or Podman), use whichever runtime you
installed with:

<CliCommand context="server">
```bash
docker logs -f miren   # or: podman logs -f miren
```
</CliCommand>

:::note[Podman and restart-on-reboot]
Docker's daemon restarts the miren container automatically after a reboot.
Podman is daemonless, so `--restart always` only covers crashes while the
machine is up — it won't bring the container back after a reboot on its own.
On a systemd host, enable `podman-restart.service`
(`sudo systemctl enable --now podman-restart.service`, or the `--user` variant
plus `loginctl enable-linger` for a rootless install), or generate a dedicated
unit with `podman generate systemd` / a Quadlet.
:::

**2. Test connectivity**

<CliCommand context="client">
```miren
miren debug connection
```
</CliCommand>

This tests RPC and HTTP connectivity to the server and reports the server version and auth status.

## Gathering a debug bundle

If you've worked through the steps above and need further help, collect a debug bundle to share:

<CliCommand context="both">
```miren
sudo miren debug bundle
```
</CliCommand>

This creates a `miren-debug.tar.gz` archive containing system info, container state, process lists, and server logs.

:::warning[Review bundles before sharing]
Debug bundles collect diagnostic data that may include sensitive information:

- **Process command lines** — arguments passed to running processes may contain tokens or credentials
- **Application logs** — error messages and stack traces can include request data or internal details

Environment variable values are automatically redacted from container inspect output, but logs and process arguments are included as-is. Review the bundle contents and remove anything sensitive before sharing, especially in public channels like GitHub Issues.
:::

:::tip[Use sudo for a complete bundle]
Without sudo, the command still runs but produces a partial bundle. Root access is needed for:
- **Containerd socket** — the primary source of container state
- **System journal** — miren server logs via journalctl
- **Docker/Podman socket** — for Docker, unless your user is in the `docker` group (rootless Podman needs no group)
:::

### Bundle options

| Flag | Description | Default |
|------|-------------|---------|
| `-o, --output` | Output file path | `miren-debug.tar.gz` |
| `-s, --since` | Include logs since this time | `1 day ago` |
| `-d, --docker-container` | Server container name (Docker or Podman) | `miren` |

<CliCommand context="both">
```miren
# Include logs from the last week
sudo miren debug bundle --since "7 days ago"

# Save to a specific path
sudo miren debug bundle -o /tmp/miren-debug.tar.gz
```
</CliCommand>

## Getting help

If you're stuck, share your debug bundle and what you've tried:

- **Discord** — [miren.dev/discord](https://miren.dev/discord) for community help and questions
- **GitHub Issues** — [File a bug report](https://github.com/mirendev/runtime/issues/new?template=bug_report.yml) and attach your debug bundle
- **Feature Requests** — [Miren Roadmap](https://github.com/mirendev/roadmap/issues) for ideas and suggestions

Remember to review debug bundles for sensitive data before attaching them to public issues.
