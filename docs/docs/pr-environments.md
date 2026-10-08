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

You don't need to configure a wildcard route on your server. Any existing route for `myapp.example.com` will pick up `pr-123.myapp.example.com` as an ephemeral lookup. See [Custom Domains](./traffic-routing.md#custom-domains) for the full DNS setup.

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

:::warning[Previews share production's backing services by default]
A preview gets its own hostname, but unless you opt in to cloning, it talks to the same databases, queues, and external services as your active version.

- Addons are shared unless you turn on cloning for them (see below).
- Database, queue, and service URLs you set by hand point at the same place they always do.
- To change a variable for just the preview, pass it at deploy time: `miren deploy --ephemeral pr-123 -e RAILS_ENV=staging`.
:::

### Giving previews their own database

You can give each preview its own copy of an addon by setting `clone = true` on it in `.miren/app.toml`:

```toml
[addons.miren-postgresql]
variant = "small"
clone = true
clone_variant = "shared" # Put preview copies on the cluster's shared server.

[addons.miren-valkey]
variant = "small"
# No clone setting, so previews share this one with the active version.
```

Each preview then gets a fresh copy of that addon's current data, with its own endpoint and credentials. Anything you don't mark with `clone = true` stays shared, including addons you attached with `miren addon create` but never listed in the file. Normal (non-preview) deploys ignore these settings and always use the app's own addons.

When you replace a preview or it expires, Miren deletes its copies. The app's own addons are never touched.

:::tip[Check for hand-set connection URLs]
Variables you set yourself win over addon bindings. If you've run something like `miren env set DATABASE_URL=...`, previews keep using that URL and never see their clone. Remove the manual variable, or override it per preview with `-e`.
:::

**Which settings apply.** A preview built from source uses the `app.toml` in that source, so a PR can opt in to cloning on its own branch. A preview made from an existing build (`miren deploy --version <version> --ephemeral <label>`) uses the settings saved with that build, not your local file.

**Picking a variant for the copies.** `clone_variant` changes the variant only for preview copies; leave it out to match the app's addon. It doesn't turn cloning on by itself and doesn't change the PostgreSQL version. The common use is giving a dedicated primary cheap previews: with `clone_variant = "shared"`, each preview gets its own database and user on the cluster's shared PostgreSQL server instead of a dedicated server of its own.

**How the copy happens.** Neither method disconnects your app or takes it offline:

- Copies onto the shared server use `pg_dump` and `pg_restore`. The dump is staged on the preview's temporary disk, so very large databases may not fit.
- Dedicated copies use `pg_basebackup` to stream a consistent snapshot of the whole server.

**Current limits.**

- PostgreSQL is the only addon that supports cloning so far. Setting `clone = true` on any other addon fails the preview rather than quietly sharing production data. Leave those addons shared, or use a [staging app](#using-a-staging-app).
- Shared-to-dedicated cloning isn't supported.
- The shared server has to run the same PostgreSQL version as your primary. If it doesn't, the preview fails and tells you why.
- Copies have a 30-minute time limit. Several previews of the same large database queue up behind each other, and one that waits too long fails; redeploy it once the others finish.

## Using a Staging App

Cloning covers PostgreSQL. Everything else a preview touches (queues, object stores, other addons, and any service URL you set yourself) is still production's. When a preview needs to be isolated from those too, run it against a staging app.

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

The preview is reachable at `pr-123.staging.myapp.example.com`, isolated from production data. Redeploy the staging app's active version periodically (or on every push to `main`) to keep its baseline fresh. Previews use the staging app's addons, not production's.

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

To deploy a preview per pull request from GitHub Actions, pair this with [CI/CD Deployment with OIDC](./ci-deploy.md) so no secrets land in your repo. The example below targets a staging app — see [Using a Staging App](#using-a-staging-app) for why that's the recommended setup.

**Step 1: Create the OIDC binding.**

`miren auth ci add --github` permits `push`, `workflow_dispatch`, and `pull_request` by default:

<CliCommand context="client">
```miren
miren auth ci add -a myapp-staging --github acme/web-app
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

- **No disk attachments** — ephemeral deploys reject configurations with disks, including storage supplied by an active addon. This prevents previews from taking production's disk leases or mounting its local or SQLite data. Diskless previews do not inherit legacy local-data auto-mounts. Existing previews with disk attachments cannot acquire new request leases or restart. This includes old previews that declared no disks but inherited a legacy local-data auto-mount. Delete affected previews or replace them with a diskless redeploy; already-running instances retain their storage until their preview is deleted or replaced.
- **`web` service only** — workers and other services from your app config don't start (see [What Runs in an Ephemeral Version](#what-runs-in-an-ephemeral-version)).
- **Shared backing services by default** — addons are shared unless you set `clone = true`, and only PostgreSQL can be cloned. Service URLs you set by hand are always shared; override them per preview with `-e`.
- **No deployment history** — `miren app history`, `miren rollback`, and the deployment lock all ignore ephemeral deploys.
- **10 per app** — older versions are evicted by expiry as new ones arrive.
- **DNS must cover the subdomains** — without a wildcard CNAME (or per-label records) pointing at your cluster, the URL won't resolve.

## Command Reference

The flags introduced on this page are `--ephemeral` and `--ttl` on [`miren deploy`](./command/deploy.md), and `--ephemeral` on [`miren app versions`](./command/app-versions.md). Those reference pages have the full flag listings.

## Next Steps

- [Deployment](./deployment.md) — How normal deploys work
- [Traffic Routing](./traffic-routing.md) — Routes, wildcard DNS, and custom domains
- [CI/CD Deployment with OIDC](./ci-deploy.md) — Deploy from GitHub Actions without stored secrets
- [TLS Certificates](./tls.md) — How HTTPS works for ephemeral subdomains
