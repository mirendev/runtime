---
title: "miren runner"
sidebar_label: "runner"
description: "Runner management commands"
---

# miren runner

Runner management commands

## Usage

```bash
miren runner [flags]
```

## Subcommands

- [`miren runner cordon`](./runner-cordon.md) — Mark a runner unschedulable without stopping its sandboxes
- [`miren runner drain`](./runner-drain.md) — Cordon a runner and evict its sandboxes onto other nodes
- [`miren runner install`](./runner-install.md) — Install systemd service for miren runner
- [`miren runner join`](./runner-join.md) — Join this machine to a coordinator as a runner
- [`miren runner list`](./runner-list.md) — List all registered runners
- [`miren runner reissue`](./runner-reissue.md) — Rotate this runner's certificate in place (requires a still-valid cert), keeping its identity
- [`miren runner remove`](./runner-remove.md) — Remove a registered runner and clean up resources
- [`miren runner service-status`](./runner-service-status.md) — Show miren-runner systemd service status
- [`miren runner start`](./runner-start.md) — Start this machine as a distributed runner
- [`miren runner status`](./runner-status.md) — Show runner health and configuration
- [`miren runner token`](./runner-token.md) — Manage join tokens
- [`miren runner uncordon`](./runner-uncordon.md) — Make a cordoned runner eligible for scheduling again
- [`miren runner uninstall`](./runner-uninstall.md) — Remove systemd service for miren runner
- [`miren runner upgrade`](./runner-upgrade.md) — Upgrade miren runner to the latest or specified version
