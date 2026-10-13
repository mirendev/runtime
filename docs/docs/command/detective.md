---
title: "miren detective"
sidebar_label: "detective"
description: "Debug with Workbench and read-only local source tools"
---

# miren detective

Debug with Workbench and read-only local source tools

Ask Workbench is a Miren Labs feature. Ask for access on Discord: https://miren.dev/discord

Ask the same cluster-scoped Workbench available in Miren Cloud. Requires a Cloud-registered cluster, a Cloud login, and Workbench enabled for the cluster's organization. Cluster selection follows the usual --cluster, MIREN_CLUSTER, per-app state, and active-cluster rules.

With no question, starts an interactive conversation; piped input is treated as one question. Pass -a to focus on an application. Responses stream as plain text. Ctrl-C aborts any active response and exits the CLI; it does not return to the chat prompt. Use /quit or Ctrl-D to exit interactive chat normally.

Detective enables read-only tools for listing, reading and searching source in the current directory (or -d). Workbench enables the same tools only with --local-tools. Requested file content is sent to Miren Cloud. There is no model-callable local shell or editing. In a Git repository, Git must be installed to resolve .git/info/exclude and the effective global excludes file, including included config and worktrees; failures refuse source access. Symlinks, gitignored files, most hidden paths, and common credential files are excluded; .miren/app.toml is eligible unless gitignored. These exclusions are not secret detection: choose a source-only directory and keep sensitive files out of it.

Local tools require the Cloud client-tool bridge. An unavailable bridge is an error, not a fallback that uploads your tree. Tools exist only while this CLI is connected; each invocation starts a new conversation.

## Usage

```bash
miren detective [args...] [flags]
```

## Flags

- `--app, -a` — Application to focus on (optional)
- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--dir, -d` — Local source root for detective or --local-tools (default: `.`)
- `--identity, -i` — Cloud identity (defaults to the cluster's identity)
- `--local-tools` — Allow read-only local source inspection; requested source content is sent to Cloud

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Inspect an app's source:**

```bash
miren detective -C production -a myapp -d ./myapp "Why does startup fail?"
```
