---
title: CI/CD Deployment
description: Deploy to Miren from CI/CD pipelines using short-lived OIDC tokens instead of stored secrets.
keywords: [ci, cd, github actions, oidc, deployment, automation, pipeline]
---

import CliCommand from '@site/src/components/CliCommand';

# CI/CD Deployment

Deploy to Miren from CI/CD pipelines without storing long-lived secrets. Instead of managing API keys or certificates, your CI system presents a short-lived OIDC identity token that Miren validates directly with the identity provider.

GitHub Actions works out of the box. Other OIDC-capable CI systems (GitLab CI, CircleCI, etc.) are supported with manual configuration.

:::info[Looking for route protection?]
This page covers OIDC for **CI/CD deployment authentication** — letting pipelines deploy without stored secrets. For putting single sign-on in front of your **application's HTTP routes**, see [Protecting Routes](/route-protect).
:::

## How It Works

1. **Create an OIDC binding** on your Miren cluster that links an app to an identity provider and subject pattern.
2. **Your CI job runs** and the Miren CLI auto-detects the CI environment (e.g., GitHub Actions sets `ACTIONS_ID_TOKEN_REQUEST_URL`).
3. **The CLI requests a short-lived token** from the CI provider's OIDC endpoint, with the cluster hostname as the audience.
4. **Miren validates the token** by fetching the provider's OIDC discovery document and JWKS keys, then checks that the token's subject and claims match a configured binding.

No secrets are stored in your CI system. The OIDC token is issued fresh for each job and expires in minutes.

:::tip[Per-PR preview deploys]
Pairing OIDC with [Pull Request Environments](/pr-environments) lets each PR get its own short-lived preview URL — see that page for the full workflow.
:::

## Quick Start with GitHub Actions

### Step 1: Get Your Cluster Address

On a machine where you're already authenticated with the cluster, export the cluster address for use in CI:

<CliCommand context="client">
```miren
miren cluster export-address
```
</CliCommand>

This outputs a string like:

```
miren.example.com:8443;sha1:a1b2c3d4e5f6...
```

The value includes the cluster address and a TLS certificate fingerprint for verification. Store this as a GitHub Actions secret named `MIREN_CLUSTER`.

### Step 2: Add an OIDC Binding

Create a binding that allows your GitHub repository to deploy:

<CliCommand context="client">
```miren
miren auth ci myapp --github acme/web-app
```
</CliCommand>

This creates a binding with:
- **Issuer:** `https://token.actions.githubusercontent.com`
- **Subject pattern:** `repo:acme/web-app:*` (matches all branches and events)
- **Allowed events:** `push,workflow_dispatch` (by default)

You can restrict further — see [Restricting Access](#restricting-access) below.

### Step 3: Add the GitHub Actions Workflow

```yaml
name: Deploy
on:
  push:
    branches: [main]

permissions:
  id-token: write    # Required — allows the job to request an OIDC token
  contents: read

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy
        uses: mirendev/actions/deploy@main
        with:
          cluster: ${{ secrets.MIREN_CLUSTER }}
          app: myapp
```

The key pieces:

- **`permissions.id-token: write`** — tells GitHub to make an OIDC token available to the job.
- **`mirendev/actions/deploy`** — installs the Miren CLI, connects to the cluster (verifying the TLS certificate fingerprint), and runs `miren deploy`. The `cluster` input takes the value from `miren cluster export-address`.
- The CLI detects `ACTIONS_ID_TOKEN_REQUEST_URL` and `ACTIONS_ID_TOKEN_REQUEST_TOKEN` in the environment, requests an OIDC token with the cluster hostname as the audience, and sends it as a bearer token.

### Alternative: Using a Config File

Instead of `MIREN_CLUSTER`, you can provide a minimal `clientconfig.yaml` with no credentials:

```yaml
active_cluster: production
clusters:
  production:
    hostname: miren.example.com:8443
```

Commit this to your repository (e.g., as `.miren/clientconfig.yaml`) and set `MIREN_CONFIG: .miren/clientconfig.yaml` in your workflow. The CLI will auto-detect OIDC the same way. `MIREN_CLUSTER` is preferred because it also verifies the TLS certificate fingerprint and requires no file in your repository.

## Restricting Access

The `--github` shorthand provides sensible defaults, but you can tighten access further.

### Subject Patterns

The subject pattern controls which GitHub Actions runs can authenticate. GitHub sets the token subject to a string like `repo:acme/web-app:ref:refs/heads/main`.

The default pattern `repo:acme/web-app:*` matches all refs and event types. To restrict to a specific branch:

<CliCommand context="client">
```miren
miren auth ci myapp --github acme/web-app \
  --subject "repo:acme/web-app:ref:refs/heads/main"
```
</CliCommand>

Glob patterns are supported. `*` matches any characters **including** `/`, unlike standard path matching. `?` matches a single character.

### Allowed Events

By default, only `push` and `workflow_dispatch` events are permitted. To also allow `pull_request` events:

<CliCommand context="client">
```miren
miren auth ci myapp --github acme/web-app \
  --allowed-events push,workflow_dispatch,pull_request
```
</CliCommand>

### Allowed Refs

Restrict deployments to specific git refs:

<CliCommand context="client">
```miren
miren auth ci myapp --github acme/web-app \
  --allowed-refs "refs/heads/main,refs/tags/v*"
```
</CliCommand>

### Claim Conditions

Claim conditions (events and refs) use the same glob pattern syntax as subject patterns. Comma-separated values mean "match any one of these alternatives."

## What OIDC Callers Can Do

OIDC-authenticated callers have a scoped set of permissions. They can:

- **Deploy** — create and manage deployments, deploy new versions
- **Build** — build from source, analyze apps
- **Read logs** — stream and view application logs
- **View app status** — check app info and configuration
- **Report telemetry** — send trace spans

OIDC callers **cannot** perform administrative operations like deleting apps, managing clusters, modifying routes, or accessing other apps.

## Other CI Platforms

For CI systems other than GitHub Actions, use the `--issuer` and `--subject` flags to create a binding:

<CliCommand context="client">
```miren
miren auth ci myapp \
  --issuer https://gitlab.com \
  --subject "project_path:acme/web-app:ref_type:branch:ref:main"
```
</CliCommand>

The CLI auto-detects OIDC tokens in GitHub Actions. For other platforms, you can adapt the following script which replicates what the GitHub Actions do — downloading the CLI, connecting to the cluster, and deploying:

```bash
#!/usr/bin/env bash
set -euo pipefail

# --- Configuration (set these in your CI environment) ---
# MIREN_CLUSTER: cluster address with TLS fingerprint (from `miren cluster export-address`)
# MIREN_APP:     application name to deploy, when the checkout has no .miren/app.toml
#                (a checked-in app.toml wins — pass `-a <name>` to override it)

: "${MIREN_CLUSTER:?MIREN_CLUSTER must be set}"
: "${MIREN_APP:?MIREN_APP must be set}"

# 1. Install the Miren CLI
MIREN_VERSION="${MIREN_VERSION:-latest}"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
esac
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"

if [ "$MIREN_VERSION" = "latest" ]; then
  DOWNLOAD_URL="https://api.miren.cloud/assets/release/miren/latest/miren-${OS}-${ARCH}.zip"
else
  DOWNLOAD_URL="https://api.miren.cloud/assets/release/miren/${MIREN_VERSION}/miren-${OS}-${ARCH}.zip"
fi

curl -fLO "$DOWNLOAD_URL"
unzip -q "miren-${OS}-${ARCH}.zip"
install -m 755 miren /usr/local/bin/
miren version

# 2. Deploy
# MIREN_CLUSTER and MIREN_APP are read from the environment automatically.
# MIREN_APP applies only if the checkout has no .miren/app.toml; that file wins.
# In GitHub Actions, the CLI auto-detects the OIDC token. For other CI systems,
# you'll need to acquire an OIDC token from your provider and set
# ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN, or
# pass the token through your platform's native mechanism.
miren deploy
```

Each CI platform has its own way of issuing OIDC tokens — check your provider's documentation for details on obtaining one.

## Managing Bindings

### List Bindings

<CliCommand context="client">
```miren
miren auth ci list -a myapp
```
</CliCommand>

Example output:

```
ID         PROVIDER   ISSUER                                        SUBJECT                   CONDITIONS
abc123     github     https://token.actions.githubusercontent.com   repo:acme/web-app:*       event_name=push,workflow_dispatch
def456     generic    https://gitlab.com                            project_path:acme/web-*
```

### Remove a Binding

<CliCommand context="client">
```miren
miren auth ci remove abc123
```
</CliCommand>

## CLI Reference

### `miren auth ci`

Create an OIDC binding for an application.

| Flag | Description |
|------|-------------|
| `-a, --app` | Application name (required) |
| `--github OWNER/REPO` | GitHub shorthand — sets issuer, subject pattern, and provider automatically |
| `--issuer URL` | OIDC issuer URL (required if `--github` is not used) |
| `--subject PATTERN` | Glob pattern for the token subject claim |
| `--allowed-events EVENTS` | Comma-separated event names to allow (default with `--github`: `push,workflow_dispatch`) |
| `--allowed-refs PATTERN` | Glob pattern for allowed git refs |
| `--description TEXT` | Human-readable description of this binding |

Either `--github` or `--issuer` is required.

### `miren auth ci list`

List OIDC bindings for an application.

| Flag | Description |
|------|-------------|
| `-a, --app` | Application name (required) |

### `miren auth ci remove`

Remove an OIDC binding by ID.

<CliCommand context="client">
```miren
miren auth ci remove <binding-id>
```
</CliCommand>

### `miren cluster export-address`

Export a cluster address with its TLS certificate fingerprint, for use with the `MIREN_CLUSTER` environment variable.

<CliCommand context="client">
```miren
# Export the active cluster
miren cluster export-address

# Export a specific cluster
miren cluster export-address -C my-cluster
```
</CliCommand>

Output format: `address:port;sha1:fingerprint`

## Environment Variables

| Variable | Description |
|----------|-------------|
| `MIREN_CLUSTER` | Cluster address with optional TLS fingerprint (`address:port;sha1:fingerprint`). The CLI connects directly — no config file needed. Can also be a cluster name from an existing config. |
| `MIREN_APP` | Target application name, used when the working directory has no `.miren/app.toml`. A config file takes precedence — use `-a myapp` to override one. |
| `MIREN_CONFIG` | Path to a `clientconfig.yaml` file. Alternative to `MIREN_CLUSTER` when you need a config file. |

The app name resolves in this order: the `-a`/`--app` flag, then `name` in `.miren/app.toml`, then `MIREN_APP`.

:::warning[MIREN_APP does not override a checked-in app.toml]
If your repository has a `.miren/app.toml`, its `name` wins over `MIREN_APP`. To target a different app from CI — a staging app, for instance — pass `-a <name>` explicitly rather than setting the environment variable.

The config file takes precedence because every app sandbox exports `MIREN_APP` naming its own app. Without this ordering, running `miren deploy` from inside a sandbox would silently target the sandbox's app instead of the checkout you are standing in.
:::

## Troubleshooting

### "OIDC token request failed" in GitHub Actions

Ensure your workflow has `permissions.id-token: write`. Without this, GitHub does not set the `ACTIONS_ID_TOKEN_REQUEST_URL` environment variable and the CLI cannot request a token.

```yaml
permissions:
  id-token: write
  contents: read
```

### "OIDC access denied" or token not matching any binding

Check that:
- The subject pattern on the binding matches the token's subject. Use `miren auth ci list` to see the configured pattern.
- GitHub's token subject format is `repo:OWNER/REPO:ref:refs/heads/BRANCH` for push events and `repo:OWNER/REPO:environment:ENVIRONMENT` for environment-triggered runs. Make sure your pattern accounts for this.
- The allowed events include the event type that triggered the workflow (e.g., `push`, `pull_request`).

### CLI not using OIDC (falling back to other auth)

OIDC auto-detection only activates when **all** of these are true:
1. The cluster config has no `identity` field.
2. The cluster config has `cloud_auth: false` (or the field is absent).
3. The environment variables `ACTIONS_ID_TOKEN_REQUEST_URL` and `ACTIONS_ID_TOKEN_REQUEST_TOKEN` are both set.

Using `MIREN_CLUSTER` satisfies conditions 1 and 2 automatically since the auto-created cluster config has no identity or cloud_auth. If you use a `clientconfig.yaml` instead, make sure it doesn't have those fields set.

### Token audience mismatch

The CLI automatically sets the token audience to the cluster hostname. Ensure the address in `MIREN_CLUSTER` (or `hostname` in your config file) exactly matches the server's hostname, including port if non-standard.

### TLS certificate fingerprint mismatch

If you see `TLS certificate fingerprint mismatch`, the server's certificate has changed since you ran `miren cluster export-address`. Re-run the export command and update your `MIREN_CLUSTER` secret with the new value.
