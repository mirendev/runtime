---
title: "miren runner reissue"
sidebar_label: "runner reissue"
description: "Rotate this runner's certificate in place (requires a still-valid cert), keeping its identity"
---

# miren runner reissue

Rotate this runner's certificate in place (requires a still-valid cert), keeping its identity

## Usage

```bash
miren runner reissue [flags]
```

## Flags

- `--config` — Path to the runner config (default: `/var/lib/miren/runner/config.yaml`)
- `--coordinator, -c` — Override coordinator address (defaults to the runner config)
- `--listen, -l` — Address this runner listens on (covered by the new cert; auto-discovered if unset)

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Rotate this runner's certificate:**

```bash
miren runner reissue
```

## See also

- [`miren runner`](/command/runner)
