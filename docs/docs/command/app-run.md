---
title: "miren app run"
sidebar_label: "app run"
description: "Open interactive shell in a new sandbox"
---

# miren app run

Open interactive shell in a new sandbox

This command runs a command in a fresh sandbox built from your app's active version — the same image, environment variables, and working directory as your deployed app — and connects your terminal to it.

With no arguments it opens an interactive shell. With arguments it runs that command. With `--task` it runs a task declared in `app.toml`.

This is useful for:
- Debugging application issues in an isolated environment
- Running one-off commands with your app's configuration
- Running a declared task by hand, such as re-running a migration without redeploying
- Exploring the container filesystem

### How It Works

1. Miren records a **run**: a durable entity holding what was executed, by whom, and how it ended
2. The run controller builds a sandbox from your app's active version and starts the command
3. Your terminal attaches to that command
4. On exit, the command's exit code is recorded and propagated to your shell, and the sandbox is torn down

### Runs outlive your terminal

Disconnecting is not cancelling. If your connection drops, the command keeps going — reattach with `miren app attach`, read its output with `miren logs run`, or leave it and let its timeout reap it.

`--detach` therefore means exactly one thing: don't attach my terminal right now. Use `miren app runs cancel` to actually stop a run.

:::tip[Sandboxes are per-invocation]
The sandbox is per-invocation. Any changes you make (files created, packages installed) are discarded when it ends.
:::

:::note[Reaching an existing sandbox]
Runs are recorded, so `miren app runs` shows what has been executed against an app and how it ended. To reach into an already-running production sandbox instead, use `miren sandbox exec`.
:::

## Usage

```bash
miren app run [args...] [flags]
```

## Flags

- `--detach` — Create the run without attaching a terminal
- `--task` — Task to run; defaults to the console task

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

## Examples

**Open a shell in your app's environment:**

```bash
miren app run
```

**Run a specific command:**

```bash
miren app run -- bin/rails console
```

**Run database migrations:**

```bash
miren app run -- bin/rails db:migrate
```

**Run a declared task:**

```bash
miren app run --task reindex
```

**Start a task without holding a terminal:**

```bash
miren app run --task reindex --detach
```

## See also

- [`miren app`](/command/app)
