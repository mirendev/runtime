---
title: "miren app run"
sidebar_label: "app run"
description: "Open interactive shell in a new sandbox"
---

# miren app run

Open interactive shell in a new sandbox

This command creates a temporary sandbox using your app's configuration (image, environment variables, working directory) and connects you to an interactive shell. The sandbox is automatically cleaned up when you exit.

This is useful for:
- Debugging application issues in an isolated environment
- Running one-off commands with your app's configuration
- Exploring the container filesystem
- Testing changes before deploying

### How It Works

1. Miren fetches your app's active version configuration
2. Creates an ephemeral sandbox with the same image, environment variables, and working directory as your deployed app
3. Waits for the sandbox to become ready
4. Connects your terminal to an interactive shell inside the sandbox
5. Cleans up the sandbox automatically when you disconnect

:::tip
The ephemeral sandbox runs independently from your production sandboxes. Any changes you make (files created, packages installed) are discarded when you exit.
:::

:::note
If you need to run commands in an existing production sandbox, use `miren sandbox exec` instead.
:::

## Usage

```bash
miren app run [args...] [flags]
```

## Config Options

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file

## App Options

- `--app, -a` — Application name (defaults to .miren/app.toml, then $MIREN_APP)
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

## See also

- [`miren app`](/command/app)
