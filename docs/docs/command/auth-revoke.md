---
title: "miren auth revoke"
sidebar_label: "auth revoke"
description: "Revoke a client certificate so the cluster refuses it"
---

# miren auth revoke

Revoke a client certificate so the cluster refuses it

Serials can be given in either base. Digits alone are read as decimal, which is how audit logs print them. A letter, a colon, or a `0x` prefix means hex, which is how openssl prints them. The confirmation prompt shows both forms so you can catch a wrong guess.

The cluster keeps no record of the certificates it issued, so it cannot tell a typo from a real serial. Check the serial against the certificate or the audit log first.

A revocation cannot be undone from the CLI. To restore someone's access, issue them a new certificate with `miren auth generate`.

The command refuses to revoke the certificate it is running with, and the coordinator's own API certificate.

A leaked runner certificate needs more than this command. It can renew itself into a new certificate with a new serial, and revoking the old serial does not touch the new one. Also run `miren runner delete` to stop further renewals, then check the audit log for other serials with that runner's subject and revoke those too.

## Usage

```bash
miren auth revoke <serial> [flags]
```

## Arguments

- `serial` — Certificate serial, in hex or decimal

## Flags

- `--cluster, -C` — Cluster name
- `--config` — Path to the config file
- `--format` — Output format (text, json) (default: `text`)
- `--json` — Shorthand for --format json
- `--reason, -r` — Why the certificate is being revoked
- `--yes, -y` — Skip confirmation prompt

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Revoke a leaked certificate:**

```bash
miren auth revoke 4f1a9c3e7b2d8a6f0c5e1d3b9a7f2e64 --reason "leaked"
```

## See also

- [`miren auth`](./auth.md)
