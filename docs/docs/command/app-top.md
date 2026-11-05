---
title: "miren app top"
sidebar_label: "app top"
description: "Show resource usage for an application"
---

# miren app top

Show resource usage for an application

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

App rows are read from the metrics store rather than from the sandboxes that
happen to be alive, so --since covers the whole window it names -- including the
deployment before the current one. An app with nothing left running is marked
"(gone)": it is in the listing only because the window reaches back far enough
to contain its samples. Samples are kept for a month, so a long enough --since
will name apps that have since been deleted.

Use "miren app top" for one app, selected from --app or the local app config.
"miren sandbox top" is the sandbox view; "miren top" remains an alias.

--series adds that app's CPU and memory over time, which a row cannot show: a
row collapses the window to one number. --contributors breaks the figures down
by the sandboxes that produced them, including the ones already replaced. Both
imply --apps under app top:

  miren app top --app shop --since 168h --series --contributors

Use --format json to get the points and the breakdown as data, and --step to
choose the resolution of the history.

--watch refreshes on a terminal. When stdout is a pipe or file, it prints one
snapshot and exits instead.

There is no restart column. Miren replaces a failed sandbox rather than
restarting it, so no single sandbox accumulates a restart count; repeated
failure shows up as a crash loop on the pool, which "miren sandbox inspect"
reports.

A row showing "-" for its numbers is running but reporting nothing. That is a
finding rather than an omission: either it has just started, or metrics
collection is down for it.

## Usage

```bash
miren app top [flags]
```

## Flags

- `--aggregate` — How to collapse the window: avg, max, min, last (default: `avg`)
- `--apps` — Show this app's totals instead of per-sandbox usage
- `--contributors` — List contributing sandboxes (implies --apps)
- `--format` — Output format (text, json) (default: `text`)
- `--interval` — Refresh interval when watching (default: `5s`)
- `--json` — Shorthand for --format json
- `--kind` — Only show sandboxes of this kind
- `--limit` — Show at most this many rows (0 for all) (default: `0`)
- `--no-addons` — Exclude dedicated addons from app totals
- `--order` — Sort direction: desc or asc
- `--runner` — Only show sandboxes on this runner
- `--samples` — Stop after this many refreshes (0 for unlimited) (default: `0`)
- `--series` — Show CPU and memory over time (implies --apps)
- `--service` — Only show sandboxes of this service
- `--since` — Measure over this window, e.g. 30s, 5m, 1h (default 1m)
- `--sort` — Sort by: cpu, memory, name, app, service, node (default: `cpu`)
- `--status` — Only show sandboxes in this status
- `--step` — Resolution of --series
- `--system` — Include addon and platform sandboxes
- `--watch, -w` — Refresh continuously until interrupted

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name
- `--dir, -d` — Directory to run from (default: `.`)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren app`](./app.md)
