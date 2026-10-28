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

That's the whole thing. Miren first honors a Dockerfile selected by `[build].dockerfile` or discovered as `Dockerfile.miren`. Without one, it uses a configured image directly or detects your language and builds one on the server, then activates the new version. If the project isn't set up yet, the CLI offers to run `miren init` first; if the app doesn't exist on the server, it's created automatically on first deploy.

## How Deployment Works

When you run `miren deploy`, Miren:

1. **Uploads your files** — sends your project files and app configuration to the server (after your first deploy, only changed files are transferred)
2. **Selects or builds an image** — builds a configured or discovered Dockerfile first; otherwise uses a configured web image directly or inspects your source for a detected [language or framework](./guides/index.md)
3. **Activates the new version** — rolls out the new version, replacing the previous one

Each deployment is a tracked object with its own ID, a status, and the git commit it came from. A successful deployment produces a new version (identified by a version ID); a failed one produces no version but stays in history as a record of the attempt. You can inspect, roll back, or redeploy any previous version at any time.

### App ownership

When a Cloud-authenticated user or service account creates an app explicitly or
through its first deployment, Miren records that principal's subject as `owned_by`.
App list and status RPC responses include this field. Subsequent deployments do
not change the owner. If two callers create a new app concurrently, the first
successful creation determines its ownership.

Existing apps without ownership remain unowned, as do apps created without a
Cloud JWT identity, including certificate, OIDC/CI, workload, and system callers.
Deployment audit attribution is separate from ownership. Deploying an unowned
app does not claim it, and Miren does not infer ownership from deployment history.
Recording ownership alone does not grant permissions or change the cluster's
current authorization rules.

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

Miren reads the app name from `.miren/app.toml`. If you haven't set up your project yet, Miren offers to run `miren init` for you. `miren init` creates `app.toml` with the app name derived from your directory, then scans the project for required environment variables and stages whatever it can — generated secrets, read-from-file values, and sensible defaults — on the app's initial config so they're available on the first deploy. See [What `miren init` Does for You](./app-configuration.md#what-miren-init-does-for-you) for the full picture. If this is the first deploy of the app, Miren creates it automatically on the server.

### Monorepos: independent deployment strategies

In a monorepo, give each independently deployed service its own `.miren` directory and app name. Services can share source code while choosing their own Dockerfile, commands, environment variables, scaling settings, and deployment targets. Unlike defining several services in one `app.toml`, these are separate Miren apps: deploying or rolling back one does not deploy or roll back the others.

Set `build.source_root` when an app needs source outside its own directory. Miren resolves it relative to the **config root**, the directory containing `.miren`, and uses the resulting **source root** as the upload root and build context. Dockerfile and top-level `include` paths are relative to the source root. Without `source_root`, the config root and source root are the same, as before.

For example, keep the API and background worker configurations alongside their shared source tree:

```text
services/
  api/
    .miren/
      app.toml
      deploy.toml
    Dockerfile
  worker/
    .miren/
      app.toml
      deploy.toml
    Dockerfile
packages/
  shared/
```

The API builds with its own Dockerfile and autoscales with incoming requests:

```toml title="services/api/.miren/app.toml"
name = "my-api"

[build]
source_root = "../.."
dockerfile = "services/api/Dockerfile"

[services.web]
command = "./bin/api"
port = 8080

[services.web.concurrency]
mode = "auto"
requests_per_instance = 20
```

The worker builds a different binary and runs two fixed instances:

```toml title="services/worker/.miren/app.toml"
name = "my-worker"

[build]
source_root = "../.."
dockerfile = "services/worker/Dockerfile"

[services.worker]
command = "./bin/worker"

[services.worker.concurrency]
mode = "fixed"
num_instances = 2
```

Here, `../..` is relative to `services/api/` or `services/worker/`, not to the `.miren` directory itself. Both apps upload and build from the repository root, so their Dockerfiles can copy code from `packages/shared/`. These Dockerfiles should build the corresponding `bin/api` and `bin/worker` executables.

Deploy from the repository root by selecting the app directory, or change into that directory and run `miren deploy`:

```miren
miren deploy -d services/api
miren deploy -d services/worker
```

The CLI projects the chosen `app.toml` into `.miren/app.toml` at the upload root without changing your repository. The server receives the usual layout and needs no new configuration-selection or path behavior.

Give the apps their own [deployment targets](#deployment-targets). For example, the API can have a staging-to-production workflow while the worker deploys to a separate jobs cluster. Run these commands from the repository root, using cluster names already configured in your client:

```miren
miren deploy target add staging api-staging-cluster -d services/api
miren deploy target add prod api-prod-cluster -d services/api
miren deploy target add prod jobs-cluster -d services/worker

miren deploy -d services/api --target staging
miren deploy -d services/api --target prod
miren deploy -d services/worker --target prod
```

Targets stay alongside each app's configuration in `services/api/.miren/deploy.toml` and `services/worker/.miren/deploy.toml`, not at the shared source root.

Each deploy command deploys only the selected app. CI jobs can use `-d services/api` or `-d services/worker` and run on independent release schedules. If CI uses path-based triggers, include shared-package changes in every dependent service's triggers; selecting an app directory does not determine which source changes affect that app.

### Deployment targets

To keep deployment destinations in the repository, add a target from the app directory:

<CliCommand context="client">
```miren
miren deploy target add staging
```
</CliCommand>

Choose a configured cluster from the picker. For scripts, supply its local name directly:

<CliCommand context="client">
```miren
miren deploy target add prod my-prod-cluster
```
</CliCommand>

Miren creates `.miren/deploy.toml` alongside `app.toml`, recording both the local cluster name and its stable Miren Cloud ID:

```toml
[[targets]]
name = "staging"
cluster = "miren-staging"
cluster_id = "cluster-abc123"

[[targets]]
name = "prod"
cluster = "miren-prod"
cluster_id = "cluster-def456"
```

The first target is the default for `miren deploy`. Select another by name:

<CliCommand context="client">
```miren
miren deploy --target prod
```
</CliCommand>

`cluster_id` is the stable Miren Cloud cluster ID. The CLI records it automatically for cloud-managed clusters, so the target resolves correctly even when different users added that cluster under different local names. The `cluster` name remains the fallback for clusters without a cloud ID.

Manage the file without editing TOML by hand:

```miren
miren deploy target                 # list targets
miren deploy target add qa          # add via cluster picker
miren deploy target set-default prod
miren deploy target remove qa
```

Deployment target commands also accept `-d` to manage another app's local targets:

```miren
miren deploy target add prod my-prod-cluster -d services/api
miren deploy target list -d services/api
miren deploy -d services/api --target prod
```

These commands use `services/api/.miren/deploy.toml`, even when `build.source_root` points to the repository root. Other apps' targets are unchanged.

An explicit `--cluster` (`-C`) or `MIREN_CLUSTER` overrides the default target. A named `--target` cannot be combined with either explicit cluster selection.

Selecting a deployment target affects that deploy only. It does not change the app's pinned cluster for commands such as `miren logs`.

### Confirmation Prompt

If your cluster config includes multiple clusters, Miren asks you to confirm which cluster to deploy to. Skip the prompt with `--force`:

<CliCommand context="client">
```miren
miren deploy --force
```
</CliCommand>

The prompt is also skipped automatically when only one cluster is configured or when stdin is not a terminal (e.g., in CI).

## Build Detection

Miren automatically detects how to build your application. It inspects your project files and identifies the language, framework, package manager, and entry points. See [Language Guides](./guides/index.md) for details on supported stacks.

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

The same source appears in `miren app status` after deployment, so it remains visible after the deploy finishes.

If a build fails, Miren displays the build errors and the deployment is marked as failed in history (visible via `miren app history`).

Use `--explain` (or `-x`) to watch each build step as it runs. This is the default in non-interactive environments; in interactive terminals, Miren shows a compact progress UI instead.

### Deploying an existing image

If your app is already packaged as a container image, set it on the `web` service:

```toml title=".miren/app.toml"
name = "myapp"

[services.web]
image = "ghcr.io/example/myapp:latest"
```

Miren normalizes the reference and launches that image directly. It does not run BuildKit, push a derived image, or create a build artifact. The container keeps the image's working directory, entrypoint, and command. If the image exposes exactly one TCP port, Miren uses it for the web service and injects the same value as `PORT`. Set `port` explicitly when the image exposes no ports or several.

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

Skip image selection and building entirely by redeploying an existing version:

<CliCommand context="client">
```miren
miren deploy --version myapp-vCVkjR6u7744AsMebwMjGU
```
</CliCommand>

Useful for rolling forward to a known-good version without waiting for a new image to resolve or build.

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
| BRANCH | Git branch (or jj bookmark) at the time of deploy |

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
✓       myapp-vCVkjR6u7744AsMebwMjGU         2m ago   Paul Hinze
✓       myapp-vCVkjJSe4fydvxEHfhsKfA         1h ago   github:acme/myapp@main
✗       myapp-vCVmuoeQCzjoNN9hGsu14c         3h ago   Paul Hinze
```

**Deployed by** shows the deployer's name from their Miren Cloud login, falling back to their email and then to the identity they authenticated as. Deploys from GitHub Actions show the repository and branch.

Status icons:
- **✓** — active (currently running) or succeeded
- **✗** — failed
- **⟳** — in progress
- **⊘** — cancelled

Show full git provenance with `--detailed`, or get JSON output for scripting. See the [`miren app history` reference](./command/app-history.md) for all options.

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

[Jujutsu](https://jj-vcs.github.io/jj/) workspaces are supported too, including non-colocated ones with no `.git` directory. The recorded commit is the parent of the working copy (`@-`), the branch is whichever local bookmark points at it, and the deploy counts as dirty when the working-copy commit (`@`) has changes on top of that parent. This matches what git reports in a colocated jj repo, so provenance looks the same either way.

If neither git nor jj can be found for the directory, the deploy still runs but warns that no provenance will be recorded, and the commit column in history shows a dash.

## Next Steps

- [Language Guides](./guides/index.md) — How Miren detects and builds different languages and frameworks
- [App Configuration](./app-configuration.md) — Configure your app with `.miren/app.toml`
- [Services](./services.md) — Define multiple processes in your app
- [CI/CD Deployment](./ci-deploy.md) — Deploy from CI pipelines with OIDC authentication
- [Pull Request Environments](./pr-environments.md) — Deploy labeled, time-boxed previews per PR
- [app.toml Reference](./app-toml.md) — Complete field reference
