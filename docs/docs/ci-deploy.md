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

1. **Create an OIDC binding** on your Miren cluster that links an app to an identity provider and the claims that identify your repository.
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
- **Repository:** `acme/web-app`, owned by `acme` (matches all branches)
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

### Restricting to a Branch

The `--github` shorthand accepts any ref from the repository, for the allowed events. To narrow it to a single branch, use `--allowed-refs`:

<CliCommand context="client">
```miren
miren auth ci myapp --github acme/web-app \
  --allowed-refs "refs/heads/main"
```
</CliCommand>

:::warning[Globs match across slashes]
`*` matches any characters including `/`, unlike standard path matching. `?` matches a single character.
:::

### A Note on Subject Patterns

Bindings created with `--github` match on the token's `repository` and `repository_owner` claims rather than on its subject. This is deliberate. As of [GitHub's immutable subject claims change](https://github.blog/changelog/2026-04-23-immutable-subject-claims-for-github-actions-oidc-tokens/), any repository created, renamed, or transferred after July 15, 2026 sends a subject with numeric IDs embedded in it:

```text
repo:acme@1234567/web-app@7654321:ref:refs/heads/main
```

A pattern written against the older `repo:acme/web-app:*` format can never match that, and GitHub recommends binding policies to repository metadata instead of parsing the subject. The `repository` claims are unaffected by the change and work under either format.

You can still set `--subject` explicitly for a provider that needs it, and it will be used as-is. If you have a binding created before this change that stopped working, re-run `miren auth ci add --github OWNER/REPO` to replace it.

:::warning[Upgrade the server first]
Older servers require a subject pattern and will reject a binding created by a newer CLI. Upgrade the server before creating or replacing a GitHub binding. Existing bindings keep working either way.
:::

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
# MIREN_APP:     application name to deploy

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
abc123     github     https://token.actions.githubusercontent.com                             repository=acme/web-app; repository_owner=acme; event_name=push,workflow_dispatch
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
| `--github OWNER/REPO` | GitHub shorthand — sets issuer, provider, and repository claim conditions automatically |
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
| `MIREN_APP` | Target application name. Equivalent to `-a myapp` on commands. |
| `MIREN_CONFIG` | Path to a `clientconfig.yaml` file. Alternative to `MIREN_CLUSTER` when you need a config file. |

## Troubleshooting

### "OIDC token request failed" in GitHub Actions

Ensure your workflow has `permissions.id-token: write`. Without this, GitHub does not set the `ACTIONS_ID_TOKEN_REQUEST_URL` environment variable and the CLI cannot request a token.

```yaml
permissions:
  id-token: write
  contents: read
```

### "OIDC access denied" or token not matching any binding

The CLI reports the rejected token's subject and repository as part of the error, so start by comparing those against `miren auth ci list -a myapp`. Then check that:

- The binding's `repository` condition names the repo the workflow is running in. A binding created for a different repo, or one left over from before a rename, won't match.
- If the binding has a subject pattern rather than repository conditions, it predates [GitHub's immutable subject claims change](https://github.blog/changelog/2026-04-23-immutable-subject-claims-for-github-actions-oidc-tokens/) and will not match a repo created, renamed, or transferred after July 15, 2026. Re-run `miren auth ci add --github OWNER/REPO` to replace it.
- The allowed events include the event type that triggered the workflow (e.g., `push`, `pull_request`).
- The allowed refs, if set, cover the branch or tag being deployed.

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
