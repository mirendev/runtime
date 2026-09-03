---
title: "miren top"
sidebar_label: "top"
description: "Show cluster-wide resource usage"
---

# miren top

Show cluster-wide resource usage

Show what a cluster is spending its CPU and memory on, ordered by the busiest.

Each row is one live sandbox. Three replicas of a service are three rows, and
dead sandboxes are left out entirely. By default only app sandboxes are listed;
addon servers and one-off task runs are hidden unless you pass --system, so the
listing answers "what are my apps doing".

CPU is reported the way top reports it: as a percentage of one core. A sandbox
using two full cores reads 200%. NODE% is the same figure against the host it
runs on, which is what says whether 200% matters.

Four flags cover most investigations:

  --nodes     Which machine is hot, split into what sandboxes are using and what
              Miren's own moving parts are using underneath them. Start here.
  --apps      One row per app instead of per sandbox, summed across everything
              that app owns. See below.
  --runner    Narrow to one host once you know which.
  --since     Look back. A live view only shows now, so a spike that has already
              passed is invisible; --since 1h --aggregate max finds it.

--apps answers "what is app X costing me" without you adding up rows. An app's
total includes the dedicated addons it owns -- a database exists because the app
asked for it -- and the SERVICES and ADDONS columns show how the total divides,
so "2.1 cores, 1.6 of it Postgres" is one line rather than an investigation.
Pass --no-addons to count only the app's own code.

There is no restart column. Miren replaces a failed sandbox rather than
restarting it, so no single sandbox accumulates a restart count; repeated
failure shows up as a crash loop on the pool, which "miren sandbox inspect"
reports.

A row showing "-" for its numbers is running but reporting nothing. That is a
finding rather than an omission: either it has just started, or metrics
collection is down for it.

## Usage

```bash
miren top [flags]
```

## Flags

- `--aggregate` — How to collapse the window: avg, max, min, last (default: `avg`)
- `--app` — Only show sandboxes belonging to this app
- `--apps` — Show per-app totals instead of per-sandbox
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--interval` — Refresh interval when watching (default: `5s`)
- `--json` — Shorthand for --format json
- `--kind` — Only show sandboxes of this kind (app, addon, run)
- `--limit` — Show at most this many rows (0 for all) (default: `0`)
- `--no-addons` — With --apps, exclude each app's dedicated addons from its total
- `--nodes` — Show per-node usage instead of per-sandbox
- `--order` — Sort direction: desc or asc (default: `desc`)
- `--runner` — Only show sandboxes on this runner (name, ID, or short ID)
- `--samples` — Stop after this many refreshes (0 for unlimited) (default: `0`)
- `--service` — Only show sandboxes of this service (e.g. web, worker)
- `--since` — Measure over this window, e.g. 30s, 5m, 1h (default 1m)
- `--sort` — Sort by: cpu, memory, app, service, node (default: `cpu`)
- `--status` — Only show sandboxes in this status
- `--system` — Include addon and platform sandboxes
- `--watch, -w` — Refresh continuously until interrupted

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**What is using the cluster's CPU:**

```bash
miren top
```

**Which host is hot, and whether an app is to blame:**

```bash
miren top --nodes
```

**Per-app totals, including each app's dedicated addons:**

```bash
miren top --apps
```

**One app's usage:**

```bash
miren top --apps --app myapp
```

**Narrow to one runner:**

```bash
miren top --runner miren-garden-runner-1
```

**Find a spike that has already passed:**

```bash
miren top --since 1h --aggregate max
```

**Watch continuously:**

```bash
miren top --watch
```

**Machine-readable output:**

```bash
miren top --format json
```
