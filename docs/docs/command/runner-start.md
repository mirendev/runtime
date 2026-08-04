---
title: "miren runner start"
sidebar_label: "runner start"
description: "Start this machine as a distributed runner"
---

# miren runner start

Start this machine as a distributed runner

## Usage

```bash
miren runner start [flags]
```

## Flags

- `--config` — Path to runner config (default: `/var/lib/miren/runner/config.yaml`)
- `--containerd-socket` — Path to containerd socket
- `--data-path` — Path to store runner data (default: `/var/lib/miren/runner`)
- `--listen, -l` — Address this runner will listen on (overrides config)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Start the runner:**

```bash
miren runner start
```

## See also

- [`miren runner`](/command/runner)
