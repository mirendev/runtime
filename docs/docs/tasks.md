---
title: Tasks
sidebar_label: Tasks
description: Run commands on Miren — once per deploy, on a schedule, or on demand — with recorded exit codes and logs.
keywords: [tasks, migrations, cron, scheduled jobs, one-off commands, miren app run]
---

# Tasks

A **service** is a process Miren keeps up. A **task** is a command Miren knows how to run.

Migrations, backfills, cleanup jobs, reindexes, and one-off scripts are tasks. Before tasks existed, these had to be smuggled in somewhere — a goroutine inside the app, a "worker" service that was really a sleep loop, or an external cron hitting an endpoint you built for the purpose. Those work until they don't, and when they stop you usually find out from the data rather than from the platform.

Each run of a task gets a fresh sandbox built from your app's deployed image and config, runs one command, records an exit code, and is torn down.

## Declaring a task

```toml
[tasks.migrate]
command = "bin/rails db:migrate"
trigger = "deploy"
timeout = "10m"
```

See the [`app.toml` reference](/app-toml#tasks) for every field.

A task always runs in your app's image and sees the same environment your services do, including credentials injected by [addons](/addons) — which is what makes the migration case work without extra configuration.

## Triggers

A task's `trigger` says what starts it.

| `trigger` | Starts when |
|-----------|-------------|
| `deploy` | A new version is deployed, before it takes traffic |
| `schedule` | A calendar expression comes due |
| `manual` | Only when you ask (the default) |

Any task can be invoked by hand whatever its trigger — that's how you re-run a migration without redeploying.

### `deploy` — run before the new version goes live

```toml
[tasks.migrate]
command = "bin/rails db:migrate"
trigger = "deploy"
```

The deploy waits. If the task fails, **the deploy fails** and your previous version keeps serving — nothing was brought up, so there is nothing to roll back. If it succeeds, the new version takes traffic.

Deploy tasks run after your addons are ready, so a migration has its database. Several of them run at the same time; ordering between tasks isn't expressible yet.

:::warning[Your previous version is running while the task is]
The gate sits before the new version takes traffic, but the task itself runs on the **new** image. So a migration executes against your database while the **old** code is still serving every request.

Your schema changes have to be compatible with both versions at once — expand-contract, or an equivalent. This is the normal path, not an edge case.
:::

:::note[Rollbacks]
Rollbacks don't run deploy tasks. Activating a version that already exists would re-run a migration against a database that's already migrated forward, and Miren doesn't run down-migrations. Reversibility is your app's to design.
:::

### `schedule` — run on a timer

```toml
# Every six hours, at 00:00, 06:00, 12:00 and 18:00 UTC
[tasks.cleanup]
command = "bin/cleanup-sessions"
trigger = "schedule"
every = "6h"

# Mondays at 9am
[tasks.report]
command = "bin/weekly-report"
trigger = "schedule"
schedule = "Mon *-*-* 09:00:00"
```

`every` and `schedule` are two spellings of the same thing and are mutually exclusive. `every` is sugar: it's converted to a calendar expression when your config is read, so that's what `miren app runs` shows you.

Scheduling has three behaviors worth knowing before you rely on it:

:::note[Ticks don't queue]
If a run is still going when the next firing comes due, that firing is **skipped** — and recorded as skipped, so `miren app runs` shows it. A job that has grown slower than its interval looks the same as a job that stopped running, unless the platform says which.
:::

:::note[Missed firings aren't replayed]
If the cluster was down when a firing came due, it's skipped rather than run on recovery. Replaying an outage's worth of jobs is a good way to turn an outage into an incident. If you need catch-up, make the job idempotent over a window.
:::

:::note[Each firing runs once]
Once across the whole cluster, without you configuring anything.
:::

### `manual` — run when you ask

```toml
[tasks.reindex]
command = "bin/reindex"
timeout = "4h"
```

```bash
miren app run --task reindex
```

## Running and watching tasks

```bash
miren app run --task reindex            # run it, attached to your terminal
miren app run --task reindex --detach   # start it, get an id back
miren app runs                          # what has run, and how it ended
miren app attach run/myapp-reindex-4kq  # join a run already going
miren logs run run/myapp-reindex-4kq    # read a run's output
miren app runs cancel run/myapp-...     # stop a run early
```

### Disconnecting is not cancelling

A run belongs to the platform, not to your terminal. If your connection drops, the command keeps going — reattach with `miren app attach`, read its output with `miren logs run`, or leave it and let its `timeout` reap it.

That gives `--detach` exactly one meaning: *don't attach my terminal right now*. To actually stop a run, use `miren app runs cancel`.

## Apps that are only tasks

An app doesn't need a long-running process at all:

```toml
name = "batch-jobs"
web = false

[tasks.reindex]
command = "bin/reindex"
```

`web = false` says so out loud. Without it, Miren would synthesize a web service from your image's entrypoint and keep it running forever — so declaring tasks with no services and no `web` setting is a build error rather than a guess.

Deploying such an app builds the image and provisions addons; that's all. Nothing runs, and nothing is billed for compute, between invocations. `miren app list` reports it as `ready` — deployed and available to invoke, as opposed to `idle`, which means an app that scaled itself to zero.

## Migrating from `[services.console]`

Declaring `[services.console]` used to get you two things: a long-running service Miren kept up, and the command `miren app run` executed. Only the second was ever useful.

Rename it:

```toml
[tasks.console]
command = "bin/rails console"
```

Until you do, Miren treats a `[services.console]` block as a task and warns at build time. That's the behavior you wanted anyway — the service was costing you a sandbox nobody used.

## Current limitations

- **A task cannot reach a Miren disk.** If your app keeps data on a [disk](./disks.md), a task can't see it — not even the disk belonging to a service in the same app. Miren disks are single-writer, and a task running beside the service that holds the lease would either block or race it, so runs skip disks entirely.

  This one is quiet: nothing warns you, and your program reports its own "no such file or directory". A backfill over disk-backed data isn't expressible yet — reach that data through an addon, or do the work inside the service that owns the disk.

- **No ordering between deploy tasks.** Every deploy task starts at once, and the deploy waits for all of them. If one migration has to land before another, put both in a single task and order them yourself.

- **A task runs in the app's image.** No per-task image. Anything your addons expose is available, since those arrive as environment variables.
