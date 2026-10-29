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

Delta uploads reuse the active version's retained source archive—the same source
used for [rebuilds](#rebuilding-the-deployed-app)—rather than storing a separate
upload cache. New and changed files are uploaded; deleted files are omitted.
After a rollback, uploads compare against the rolled-back version. If retained
source is unavailable, Miren uploads the full project instead.

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

## Rebuilding the Deployed App

To pick up updated base images without uploading a new working tree:

<CliCommand context="client">
```miren
miren app rebuild -a myapp
```
</CliCommand>

Miren rebuilds the active version's source retained on the server. This works
with both automatically detected stacks and Dockerfile builds, including custom
Dockerfile paths. Mutable base image tags are resolved again; digest-pinned
images remain pinned. BuildKit can still reuse layers whose inputs have not
changed.

The rebuild preserves the current runtime configuration, runs deploy-triggered
tasks, and rolls out a new version using the usual deployment lock and health
checks. Local files and local Git state are not used. Add `-m "Refresh base images"`
to record a message in deployment history, or `--format json` for machine-readable
output. Use `miren deploy cancel -a myapp` to cancel an in-progress rebuild.

:::info[Source retention required]
Apps deployed before source retention was available need one normal
`miren deploy` with an updated server before they can be rebuilt.
If the server cannot retain source, it logs a warning but allows the deployment
to continue. That version cannot be rebuilt, and subsequent uploads fall back to
uploading the full project. Restore source storage and deploy again to enable
rebuilds for the new version.
:::

Apps that run a prebuilt image directly cannot be rebuilt; deploy them again to
resolve their image tag. Unlike a rebuild, `miren app restart` reuses the existing image.

### Choosing dependency pins for security updates

Pin the **compatibility boundary** you support, rather than every artifact's exact
bytes, when you want rebuilds to pick up upstream fixes. A rebuild can only use a
fix published within that boundary. It does not decide which releases are
security fixes, widen version constraints, or schedule itself: run
`miren app rebuild` when you want to refresh an app.

#### OCI base images and language runtimes

For Dockerfile builds, choose a maintained tag that follows the runtime release
line and OS distribution you have tested. For example:

```dockerfile
FROM python:3.13-slim-bookworm
```

This allows the image publisher to deliver Python 3.13 patch releases and Debian
Bookworm image updates without moving the app to Python 3.14 or a different
distribution. Apply the same policy to **every stage** in a multi-stage Dockerfile,
including builder images: a patched final image does not fix a vulnerable compiler
or library copied from the builder.

| Image reference | What a rebuild can pick up |
| --- | --- |
| `python:3.13-slim-bookworm` | New image contents published under that tag, including runtime patches and OS fixes |
| `python:3.13.1-slim-bookworm` | New contents under that exact tag, if republished; not a later Python patch version |
| `python:3.13-slim-bookworm@sha256:…` | Nothing newer: the digest fixes the image contents even when a tag is also present |
| `python:latest` | Whatever the publisher makes latest, potentially including incompatible runtime or distribution changes |

Avoid `latest` for production merely to get security updates. Prefer a supported,
bounded release line, and plan explicit upgrades before it reaches end of life.
Tags are publisher-defined names, not version constraints: Miren resolves the
same tag again; it does not search for newer tags. A security fix must actually
be published under the selected tag.

For automatic stack builds, select the runtime line through `[build] version` in
`.miren/app.toml`, where supported by your [language stack](./guides/index.md):

```toml
[build]
version = "3.13"
```

For Python this selects the `python:3.13-slim` base image through Miren's image
proxy. Other stacks have their own version mappings; do not assume every stack
accepts semver ranges or maps a short version to a floating image tag. For example,
the Elixir stack maps supported Elixir/OTP combinations to specific dated builder
tags, so moving that builder to a newer release requires an updated Miren runtime.

#### Language packages and lockfiles

Choose bounded version constraints that permit compatible fixes. These examples
allow patch releases within the 1.2 line; replace the versions with the supported
line for each dependency:

| Package specification | Patch-compatible constraint |
| --- | --- |
| npm, Yarn, or Bun `package.json` | `"some-package": "~1.2.3"` |
| pip `requirements.txt` | `some-package>=1.2.3,<1.3` |
| Ruby `Gemfile` | `gem "some-package", "~> 1.2.3"` |
| Rust `Cargo.toml` | `some-package = "~1.2.3"` |
| Elixir `mix.exs` | `{:some_package, "~> 1.2.3"}` |

If your compatibility policy allows minor updates too, use a wider bounded range,
such as npm's `^1.2.3` or pip's `>=1.2.3,<2`. A fix released outside your range
still requires changing the specification. Version ranges express compatibility,
not a guarantee that an upstream release is safe or contains a security fix.

**A range does not override a lockfile.** Stack builds use normal install commands
such as `npm install`, `bun install`, `bundle install`, `uv sync`, and
`cargo build`; rebuild does not replace them with dependency-update commands.
An existing compatible locked version generally stays selected. This applies to
files such as `package-lock.json`, `yarn.lock`, `bun.lock`, `Gemfile.lock`,
`poetry.lock`, `uv.lock`, `Pipfile.lock`, `Cargo.lock`, and `mix.lock`. Go's
`go.mod` selects dependency versions rather than declaring floating semver ranges;
`go mod download` does not upgrade them. `go.sum` records checksums, not version
selections. Vendored dependencies are retained source too and do not refresh on
rebuild. A new runtime can cause some installers to adjust a resolution for
compatibility, but that is not a general security update.

For reproducible application dependencies, **keep lockfiles** and use a dependency
update bot or a regular update-and-test workflow. Commit the updated specifications,
lockfiles, or vendor tree, then run `miren deploy` to upload them. A server-side
rebuild cannot see a lockfile changed only in your local checkout or Git repository.

If you deliberately choose unlocked dependencies, a fresh install can select newer
versions within the ranges, but you give up exact reproducibility. Do not remove a
lockfile solely to make rebuilds appear to update dependencies: it may also affect
stack detection and transitive dependency resolution.

:::warning[Rebuild is not a dependency upgrade or a cache purge]
BuildKit can reuse an unchanged dependency-install layer even for an unlocked
specification. When a base image changes, steps that depend on it are normally
rebuilt, but the mere passage of time or a new package-registry release does not
invalidate a cached install. Dockerfile `RUN` steps follow the same rule, including
OS package installation. Rebuild is therefore a way to pick up refreshed base
images, not a guarantee that every application or OS package is upgraded.
:::

Use refreshed supported base images **and** regular dependency updates together.
Test the resulting app and monitor deployment health; keep the previous version
available for rollback. If you require digest-pinned images for reproducibility,
have your update workflow change those digests explicitly and deploy the change.

#### Maintaining package specifications and lockfiles

Treat the package specification as your compatibility policy and the lockfile as
the concrete dependency set you have tested. Maintain both:

1. **Declare supported ranges.** List your direct dependencies in `package.json`,
   `pyproject.toml`, `Gemfile`, `Cargo.toml`, or `mix.exs`. Keep the lower bound high
   enough to exclude known-vulnerable releases, and widen the upper bound
   deliberately when a fix requires a newer release line. For Go, update the
   selected module version in `go.mod` using Go tooling.
2. **Refresh the resolution regularly.** Use a dependency update bot such as
   Dependabot or Renovate where it supports your ecosystem, or run your package
   manager's update command. Include transitive dependencies: a vulnerable package
   may appear only in the lockfile, not in your direct dependency list.
3. **Review and test the result.** Review changed versions, checksums, and upstream
   release notes; run your tests and vulnerability checks. A version range permits
   an update but does not prove it is compatible. If a transitive fix cannot be
   selected, update the parent dependency or deliberately adjust its constraints
   rather than hand-editing the lockfile.
4. **Commit the inputs together.** Commit changed package specifications and the
   generated lockfile in the same change. Use the project's package-manager
   version, resolve lockfile conflicts with that tool, and regenerate any vendor
   tree. Avoid deleting a lockfile to resolve a small update or merge conflict:
   that can unexpectedly re-resolve the entire dependency graph.
5. **Deploy the updated source.** Run `miren deploy`, test the deployed app, and
   monitor health. Running `miren app rebuild` instead would reuse the old retained
   specifications and lockfiles, even if you have already committed newer ones.

Examples of targeted updates, run in your project checkout (replace `PACKAGE`
with the dependency name):

| Ecosystem | Update workflow |
| --- | --- |
| npm | `npm update PACKAGE` updates within declared ranges; change the declaration explicitly if the fix is outside them |
| Yarn | Yarn 1: `yarn upgrade PACKAGE`; Yarn 2+: `yarn up PACKAGE` (review declaration changes as well as the lockfile) |
| Bun | `bun update PACKAGE`; review `package.json` and `bun.lock` |
| uv | `uv lock --upgrade-package PACKAGE`, then `uv sync` to test that resolution |
| Poetry | `poetry update PACKAGE`; review `pyproject.toml` and `poetry.lock` |
| Pipenv | `pipenv update PACKAGE`; review `Pipfile` and `Pipfile.lock` |
| Ruby / Bundler | `bundle update PACKAGE`; review `Gemfile` and `Gemfile.lock` |
| Rust / Cargo | `cargo update -p PACKAGE`; update `Cargo.toml` too if its constraint excludes the fix |
| Elixir / Mix | `mix deps.update PACKAGE`; review `mix.exs` and `mix.lock` |
| Go | `go get PACKAGE@vX.Y.Z`, then `go mod tidy`; run `go mod vendor` if you vendor dependencies, and commit the changed `go.mod`, `go.sum`, and vendor files |

For pip, distinguish a hand-maintained requirements list from a generated locked
one. If you use pip-tools, keep direct dependency constraints in `requirements.in`
and run `pip-compile --upgrade-package PACKAGE` to regenerate `requirements.txt`.
Commit both files and install from the generated requirements for testing. If you
maintain `requirements.txt` directly, update its pins or ranges explicitly and
check transitive dependencies too: pinning only direct dependencies does not lock
the complete environment. Preserve any hash-checking policy when regenerating.

Use a regular update cadence plus prompt updates for security advisories. Do not
rely solely on a successful build or a clean vulnerability scan: neither guarantees
that all dependencies are secure or that behavior is unchanged.

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
