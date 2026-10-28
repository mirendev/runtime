---
title: CLI compatibility
description: Updated command names, cluster selection, and the transition for existing scripts.
---

# CLI compatibility

Miren commands use singular categories with explicit verbs. Categories such as `miren route`, `miren cluster`, `miren deployment`, and `miren target` print help; use `list` to see their objects.

## Command names

| Previous form | Preferred form | Compatibility |
| --- | --- | --- |
| `deploy` | `deployment create` | Permanent alias |
| `rollback` | `app rollback` | Permanent alias |
| `top` | `sandbox top` | Permanent alias |
| `login`, `logout` | `cloud login`, `cloud logout` | Permanent aliases |
| `server` | `server start` | Permanent alias; existing systemd units still work |
| `admin METHOD` | `app admin run METHOD` | Permanent alias |
| `admin --list` | `app admin list` | Existing form remains supported |
| `app` / `app --watch` | `app status` / `app watch` | Dashboard retained with warning for one release |
| `apps` | `app list` | Deprecated alias for one release |
| `app attach`, `app runs`, `app runs cancel` | `run attach`, `run list`, `run cancel` | Deprecated aliases for one release |
| `app versions` | `app history --versions` | Old output retained with warning for one release |
| `deploy cancel -d ID` | `deployment cancel ID` | Old name and flag retained with warnings |
| `deploy target add/list/remove` | `target add/list/remove` | Deprecated aliases for one release |
| `addon destroy`, `secret destroy` | `addon delete`, `secret delete` | Deprecated aliases for one release |
| `route remove`, `runner remove` | `route delete`, `runner delete` | Deprecated aliases for one release |
| `sandbox-pool` | `debug sandbox-pool` | Old subcommands retained with warnings |
| `download release` | `server download-release` | Deprecated alias for one release |
| `top --app NAME` | `app top -a NAME` | Old flag retained with warning |
| `route set HOST APP` | `route set HOST -a APP` | Positional app retained with warning |
| `app delete APP` | `app delete -a APP` | Positional app retained with warning |

When a legacy positional app disagrees with `--app` or `MIREN_APP`, `app delete` and `route set` refuse to run. Remove the conflicting selector rather than relying on one to silently override the other.

`app status --graph` adds a one-time resource snapshot. `app watch` is live only when stdout is a terminal; piped output prints once and exits. `app pool-size [COUNT] --service web` shows or changes an active service's desired sandbox count. Autoscaling can subsequently change that count; use fixed concurrency in `app.toml` for a persistent setting.

`app history --ephemeral` lists preview versions, including versions without deployment records. Existing `app versions` JSON retains its shape, with an optional `short_id` added when available. List output includes short IDs for use in subsequent commands, falling back to full IDs for older servers.

## Choosing a cluster

All app-centric commands follow the same [deployment target selection rules](./deployment.md#deployment-targets). `-C` and `MIREN_CLUSTER` select a target when `deploy.toml` exists. Target order no longer supplies a default, and per-app pins in `app-state.toml` are ignored but not deleted.

Without targets, other app commands may use the global active cluster. Deploy requires explicit selection, a sole configured cluster, or a picker; ambiguous unattended deploys fail rather than silently using the global active cluster. `cluster switch` changes only the global active cluster.

With no configured clusters, cluster-backed commands explain how to run `miren cluster add` rather than implicitly dialing the default local development address. For direct-address development without client configuration, pass `--server-address 127.0.0.1:8443` explicitly. This does not bypass an invalid or ambiguous deployment-target selection. JSON deploy modes require an unambiguous selection and never open a picker, even on a terminal.

## Confirmation and JSON

Use `--yes` to skip confirmations. On deploy and app, sandbox, env, addon, and secret deletion, the old confirmation-skipping `-f/--force` remains a deprecated alias for one release. Runner deletion's `--force` still overrides its active-sandbox safety check; it does **not** skip confirmation.

Route deletion, cluster removal, and runner deletion now confirm on an interactive terminal. For one release, unattended calls to these commands continue without `--yes`, warning that it will be required in the next release. Commands that already required confirmation refuse unattended deletion unless `--yes` (or its deprecated force alias) is supplied.

Every transition warning goes to stderr so JSON on stdout stays parseable. Use `--json` or `--format json`; admin's `-j` remains supported with a warning.
