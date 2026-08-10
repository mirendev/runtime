---
title: Pull Request Environments
description: Deploy a labeled, time-boxed preview of your app on a subdomain — one per pull request, with automatic cleanup.
keywords: [pr, preview, ephemeral, github, pull request, environment, review app]
---

import CliCommand from '@site/src/components/CliCommand';

# Pull Request Environments

A pull request environment is a labeled build of your app — called an **ephemeral version** in Miren — that runs alongside the active version on its own subdomain. It runs separately from your normal deploys and is deleted automatically when its TTL expires. The typical use is one preview per PR, reachable at something like `pr-123.myapp.example.com`.

## Minimum working example

With wildcard DNS pointed at your cluster (`*.myapp.example.com` → your cluster's hostname), one flag creates a preview:

<CliCommand context="client">
```miren
miren deploy --ephemeral pr-123 --ttl 48h
```
</CliCommand>

The build comes up at `https://pr-123.myapp.example.com`, runs alongside the active version without touching production traffic, and deletes itself when the TTL expires. See [Quick Start](#quick-start) for the DNS setup.

## How It Works

When you run `miren deploy --ephemeral <label>`:

1. Miren builds the version but doesn't activate it. The active version keeps serving production traffic.
2. The new version is reachable at `<label>.<your-app-host>`. A request for any subdomain of an existing route is looked up against ephemeral labels for that route — no separate route entity needed.
3. After the TTL elapses (default 24 hours), a background controller deletes the version.

Ephemeral deploys don't create deployment history records, don't take the deployment lock, and don't block normal deploys.

## Quick Start

**Step 1: Point DNS at your cluster for the subdomains you'll use.** A wildcard CNAME is the usual choice:

```text
*.myapp.example.com.   CNAME   cluster-jwomf2l0tn8z.miren.systems.
```

You don't need to configure a wildcard route on your server. Any existing route for `myapp.example.com` will pick up `pr-123.myapp.example.com` as an ephemeral lookup. See [Custom Domains](/traffic-routing#custom-domains) for the full DNS setup.

**Step 2: Deploy with `--ephemeral` and an optional TTL.**

<CliCommand context="client">
```miren
miren deploy --ephemeral pr-123 --ttl 48h
```
</CliCommand>

Miren builds the version and prints the access URL:

```text
Ephemeral version myapp-vXYZ created.
  Label: pr-123
  TTL:   48h
  URL:   https://pr-123.myapp.example.com
```

Open the URL — your preview is live. TLS provisions on first request.

## What Runs in an Ephemeral Version

:::warning[Only the web service runs]
Workers, background jobs, scheduled tasks, and any other services defined in `.miren/app.toml` aren't started for ephemeral versions — HTTP traffic to the subdomain is the only thing wired up. If reviewing your PR requires a worker too, use a separate staging app instead.
:::

:::warning[Configuration is shared with the active version]
Env vars, secrets, addons, and build config all come from the app's current settings. There's no per-ephemeral override:

- Your preview connects to the same database, queues, and external services as production.
- You can't change `RAILS_ENV`, feature flags, or service URLs just for the preview.
- Updates to app config (via `miren config set`, addons, etc.) affect both active and ephemeral versions.
:::

If you need isolation from production data, run ephemeral deploys against a separate staging app.

## Using a Staging App

Because ephemeral versions share config with the active version, a preview deployed against your production app talks to your production database, queues, and external services. That's fine for read-mostly UI changes, but risky as soon as a PR writes data, runs migrations, or fires background jobs.

Set up a second app — typically `myapp-staging` — that points at a staging database and any other backing services you want isolated, then run all PR previews against that app instead of production.

**Step 1: Create the staging app.** Deploy your main branch to it with whatever staging-specific config you want:

<CliCommand context="client">
```miren
miren deploy -a myapp-staging \
  -e DATABASE_URL=postgres://staging-db.internal/myapp \
  -e RAILS_ENV=staging
```
</CliCommand>

**Step 2: Add a route for the staging app and wildcard DNS for its subdomains.**

<CliCommand context="client">
```miren
miren route set staging.myapp.example.com myapp-staging
```
</CliCommand>

```text
*.staging.myapp.example.com.   CNAME   cluster-jwomf2l0tn8z.miren.systems.
```

**Step 3: Run PR previews against the staging app.**

<CliCommand context="client">
```miren
miren deploy -a myapp-staging --ephemeral pr-123 --ttl 48h
```
</CliCommand>

The preview is reachable at `pr-123.staging.myapp.example.com`, isolated from production data. Redeploy the staging app's active version periodically (or on every push to `main`) to keep its baseline fresh — ephemeral previews inherit the staging app's current config and addons, not production's.

In CI, set `MIREN_APP=myapp-staging` (or pass `app: myapp-staging` to the deploy action) so PR workflows always target staging.

### Using a Staging Cluster

Another pattern is to setup a separate staging cluster that you deploy the PRs to, rather than your production cluster. This lets you isolate the preview even more.

## Labels

Labels are used as DNS subdomains, so they must be DNS-compliant (RFC 1123):

- Lowercase alphanumeric characters and hyphens only
- Must start and end with an alphanumeric character
- Max 63 characters

Miren normalizes common separators for you — underscores, slashes, and dots become hyphens, uppercase is lowercased, and other characters are stripped:

| Input | Normalized |
|-------|------------|
| `feat/login` | `feat-login` |
| `My_Branch.v2` | `my-branch-v2` |
| `PR-123` | `pr-123` |

So a Git branch name usually works as-is:

<CliCommand context="client">
```miren
miren deploy --ephemeral "$(git rev-parse --abbrev-ref HEAD)"
```
</CliCommand>

:::info[Redeploying with the same label replaces the prior version]
Push a new commit, redeploy with `--ephemeral pr-123`, and the previous `pr-123` version is deleted before the new one becomes reachable. The TTL resets on each deploy.
:::

## TTL and Cleanup

`--ttl` takes a Go duration string (`30m`, `2h`, `48h`) and defaults to `24h`. Expiration is fixed at deploy time.

Requests to an expired label return 404 immediately — the ephemeral lookup filters expired versions itself, so the cutoff is enforced as soon as the timestamp passes. A background controller sweeps the actual entities every five minutes to free their resources. There's no extend command — redeploy with the same label to refresh the TTL.

**Per-app limit.** Each app can have at most 10 ephemeral versions at once. Deploying an 11th evicts the version nearest to expiry. Replacing an existing label doesn't count against the limit, since the old version is deleted before the new one is created.

## Listing Ephemeral Versions

Show only ephemeral versions for an app:

<CliCommand context="client">
```miren
miren app versions --ephemeral
```
</CliCommand>

```text
VERSION                              LABEL    CREATED   EXPIRES
myapp-vCVkjR6u7744AsMebwMjGU         pr-123   2m ago    2026-05-28 14:00:00
myapp-vCVkjJSe4fydvxEHfhsKfA         pr-118   3h ago    2026-05-28 11:30:00
```

`--format json` is supported for scripting. Drop `--ephemeral` to see all versions, with ephemeral ones marked.

Ephemeral deploys don't appear in `miren app history` — that command shows only tracked deployments of the active version.

## GitHub Actions: Per-PR Previews

To deploy a preview per pull request from GitHub Actions, pair this with [CI/CD Deployment with OIDC](/ci-deploy) so no secrets land in your repo. The example below targets a staging app — see [Using a Staging App](#using-a-staging-app) for why that's the recommended setup.

**Step 1: Allow `pull_request` events on the OIDC binding.**

`miren auth ci add --github` permits `push` and `workflow_dispatch` by default. Add `pull_request`:

<CliCommand context="client">
```miren
miren auth ci add -a myapp-staging --github acme/web-app \
  --allowed-events push,workflow_dispatch,pull_request
```
</CliCommand>

**Step 2: Add the workflow.**

```yaml
name: PR Preview
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  id-token: write
  contents: read

jobs:
  preview:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy preview
        id: deploy
        uses: mirendev/actions/deploy@main
        with:
          cluster: ${{ secrets.MIREN_CLUSTER }}
          app: myapp-staging
          ephemeral: pr-${{ github.event.pull_request.number }}
          ttl: 48h
```

Each push replaces the previous preview at the same label. When the PR is merged or closed, the version expires on its own — no teardown step needed.

The deploy action exposes the preview URL as a step output, so a follow-up step can post it as a PR comment:

```yaml
      - name: Comment preview URL
        uses: actions/github-script@v7
        with:
          script: |
            const url = '${{ steps.deploy.outputs.url }}';
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `Preview deployed: ${url}`,
            });
```

## Limitations

- **`web` service only** — workers and other services from your app config don't start (see [What Runs in an Ephemeral Version](#what-runs-in-an-ephemeral-version)).
- **Shared configuration** — env vars, secrets, and addons all come from the app. No per-preview overrides.
- **No deployment history** — `miren app history`, `miren rollback`, and the deployment lock all ignore ephemeral deploys.
- **10 per app** — older versions are evicted by expiry as new ones arrive.
- **DNS must cover the subdomains** — without a wildcard CNAME (or per-label records) pointing at your cluster, the URL won't resolve.

## Command Reference

The flags introduced on this page are `--ephemeral` and `--ttl` on [`miren deploy`](/command/deploy), and `--ephemeral` on [`miren app versions`](/command/app-versions). Those reference pages have the full flag listings.

## Next Steps

- [Deployment](/deployment) — How normal deploys work
- [Traffic Routing](/traffic-routing) — Routes, wildcard DNS, and custom domains
- [CI/CD Deployment with OIDC](/ci-deploy) — Deploy from GitHub Actions without stored secrets
- [TLS Certificates](/tls) — How HTTPS works for ephemeral subdomains
