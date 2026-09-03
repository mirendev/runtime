---
title: Deployment
description: How miren deploy uploads an app, selects or builds an image, and activates new versions.
keywords: [deploy, build, rollback, versions, container image]
---

import CliCommand from '@site/src/components/CliCommand';

# Deployment

Deployment is the core workflow of Miren. It takes your application source or configured container image and runs it on your server.

## Minimum working example

From the root of your project:

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

That's the whole thing — Miren detects your language, builds the image on the server, and activates the new version. If the project isn't set up yet, the CLI offers to run `miren init` first; if the app doesn't exist on the server, it's created automatically on first deploy.

## How Deployment Works

When you run `miren deploy`, Miren:

1. **Uploads your files** — sends your project files and app configuration to the server (after your first deploy, only changed files are transferred)
2. **Selects or builds an image** — uses a configured web image directly, or inspects your source to build an image from a detected [language or framework](/guides) or your Dockerfile
3. **Activates the new version** — rolls out the new version, replacing the previous one

Each deployment is a tracked object with its own ID, a status, and the git commit it came from. A successful deployment produces a new version (identified by a version ID); a failed one produces no version but stays in history as a record of the attempt. You can inspect, roll back, or redeploy any previous version at any time.

## Deploying from a Project Directory

The most common workflow — run `miren deploy` from the root of your project:

<CliCommand context="client">
```miren
cd ~/myapp
miren deploy
```
</CliCommand>

You can also deploy from a different directory with `-d`:

<CliCommand context="client">
```miren
miren deploy -d path/to/app
```
</CliCommand>

Miren reads the app name from `.miren/app.toml`. If you haven't set up your project yet, Miren offers to run `miren init` for you. `miren init` creates `app.toml` with the app name derived from your directory, then scans the project for required environment variables and stages whatever it can — generated secrets, read-from-file values, and sensible defaults — on the app's initial config so they're available on the first deploy. See [What `miren init` Does for You](/app-configuration#what-miren-init-does-for-you) for the full picture. If this is the first deploy of the app, Miren creates it automatically on the server.

### Confirmation Prompt

If your cluster config includes multiple clusters, Miren asks you to confirm which cluster to deploy to. Skip the prompt with `--force`:

<CliCommand context="client">
```miren
miren deploy --force
```
</CliCommand>

The prompt is also skipped automatically when only one cluster is configured or when stdin is not a terminal (e.g., in CI).

## Build Detection

Miren automatically detects how to build your application. It inspects your project files and identifies the language, framework, package manager, and entry points. See [Language Guides](/guides) for details on supported stacks.

Use `--analyze` to see what Miren detects without actually building or deploying:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

You'll see the selected source, services, entrypoint, and what files and frameworks influenced the result. Automatic builds name the detected stack, while image deploys show the normalized image reference:

```text
Source: python (auto-detected)
Source: dockerfile
Source: image ghcr.io/example/myapp:latest
```

The same source appears in `miren app status` after deployment, so it remains visible after the build finishes.

If a build fails, Miren displays the build errors and the deployment is marked as failed in history (visible via `miren app history`).

Use `--explain` (or `-x`) to watch each build step as it runs. This is the default in non-interactive environments; in interactive terminals, Miren shows a compact progress UI instead.

### Deploying an existing image

If your app is already packaged as a container image, set it on the `web` service:

```toml title=".miren/app.toml"
name = "myapp"

[services.web]
image = "ghcr.io/example/myapp:latest"
port = 8080
```

Miren normalizes the reference and launches that image directly. It does not run BuildKit, push a derived image, or create a build artifact. When `command` is unset, the container uses the image's default entrypoint and command.

:::note[Image selection]
A Dockerfile selected by `[build].dockerfile` or discovered as `Dockerfile.miren` takes precedence over `services.web.image`. Without one, the web image takes precedence over automatic source detection, even if the project directory also contains recognizable source files. Images on other services do not suppress source detection. If detection finds no buildable source, Miren uses a lone service image as the fallback; with several service images, set `services.web.image` to choose the primary one.
:::

## Deploy-Time Environment Variables

Set environment variables at deploy time with `-e` and `-s`. They're applied to the app before the new version activates.

<CliCommand context="client">
```miren
# Regular variables
miren deploy -e RELEASE_SHA=abc123 -e LOG_LEVEL=debug

# Sensitive variables (masked in output)
miren deploy -s DATABASE_URL=postgres://user:pass@host/db

# Read value from a file
miren deploy -s API_KEY=@secrets/api-key.txt

# Prompt for the value (sensitive vars mask input)
miren deploy -s SECRET_KEY
```
</CliCommand>

## Redeploying an Existing Version

Skip the build entirely and redeploy a previously built version:

<CliCommand context="client">
```miren
miren deploy --version myapp-vCVkjR6u7744AsMebwMjGU
```
</CliCommand>

Useful for rolling forward to a known-good version without waiting for a new build.

Find version IDs with `miren app history` (see [Deployment History](#deployment-history) below).

## Rollback

`miren rollback` provides an interactive way to revert to a previous version:

<CliCommand context="client">
```miren
miren rollback -a myapp
```
</CliCommand>

This presents a picker showing your recent successful deployments:

| Column | Description |
|--------|-------------|
| VERSION | The version ID |
| STATUS | Deployment status (active, succeeded) |
| WHEN | Relative timestamp |
| GIT SHA | Short commit hash |
| BRANCH | Git branch at the time of deploy |

Select a version and Miren redeploys it immediately. The currently active version is excluded from the list since rolling back to the current version would be a no-op.

Rollback creates a new deployment record — it doesn't erase history.

## Deployment History

View the history of deployments for an app:

<CliCommand context="client">
```miren
miren app history -a myapp
```
</CliCommand>

### Output

```text
STATUS  VERSION                              WHEN     DEPLOYED BY
✓       myapp-vCVkjR6u7744AsMebwMjGU         2m ago   paul@miren.dev
✓       myapp-vCVkjJSe4fydvxEHfhsKfA         1h ago   paul@miren.dev
✗       myapp-vCVmuoeQCzjoNN9hGsu14c         3h ago   paul@miren.dev
```

Status icons:
- **✓** — active (currently running) or succeeded
- **✗** — failed
- **↩** — rolled back
- **⟳** — in progress
- **⊘** — cancelled

Show full git provenance with `--detailed`, or get JSON output for scripting. See the [`miren app history` reference](/command/app-history) for all options.

## Cancelling a Deployment

Cancel an in-progress deployment by its deployment ID:

<CliCommand context="client">
```miren
miren deploy cancel -d <deployment-id>
```
</CliCommand>

Get the deployment ID from `miren app history --detailed`. If a CLI session is watching that deployment, it'll detect the cancellation and exit cleanly.

:::warning[One deployment at a time]
Only one deployment can run per app at a time. If you attempt to deploy while another is in progress, Miren tells you who started it and when. You can wait for it to finish or cancel it and start a new one.
:::

## Git Provenance

Miren automatically captures git metadata (commit, branch, author, dirty state) from your working directory at deploy time. This information appears in `miren app history --detailed` and in the `miren rollback` picker. No configuration is needed — if you deploy from a git repo, provenance is captured automatically.

## Next Steps

- [Language Guides](/guides) — How Miren detects and builds different languages and frameworks
- [App Configuration](/app-configuration) — Configure your app with `.miren/app.toml`
- [Services](/services) — Define multiple processes in your app
- [CI/CD Deployment](/ci-deploy) — Deploy from CI pipelines with OIDC authentication
- [Pull Request Environments](/pr-environments) — Deploy labeled, time-boxed previews per PR
- [app.toml Reference](/app-toml) — Complete field reference
