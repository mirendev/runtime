---
title: "miren cluster add"
sidebar_label: "cluster add"
description: "Add a new cluster configuration"
---

# miren cluster add

Add a new cluster configuration

With `--format json`, the command prints one result document and nothing else on stdout; progress and warnings go to stderr. A failure is reported both as a document and as a non-zero exit status.

```json
{"ok": true, "cluster": {"name": "prod", "xid": "cluster-...", "organization": "Acme",
                          "address": "10.0.0.1:8443", "via_cloud": false,
                          "identity": "cloud", "active": true,
                          "config_file": "~/.config/miren/clientconfig.d/prod.yaml"}}

{"ok": false, "error": {"code": "cluster_not_found", "message": "no cluster named ..."}}
```

`name` is the local name, which is what every other command takes. `cloud_name` appears alongside it only when `--as` stored the cluster under a different name than it has in Miren Cloud, and `address` is absent for a cluster reached through cloud.

Messages are written for people and will be reworded. The code is the stable part:

| Code | Meaning |
|------|---------|
| `invalid_flags` | The flags given can't mean anything together. |
| `interactive_required` | Answering needs a person. Name a cluster with `--cluster`, or use `--force` to overwrite. |
| `no_identities` | Nobody is logged in. Run `miren login`. |
| `identity_error` | The identity named wasn't found, or one has to be named with `--identity`. |
| `cloud_request_failed` | Miren Cloud didn't answer. Worth retrying. |
| `cluster_not_found` | No cluster by that name, or none on the account. |
| `ambiguous_cluster` | The name exists in more than one organization. Add `--organization`. |
| `unknown_organization` | No organization by that name. |
| `cluster_unreachable` | The cluster exists and couldn't be reached. Worth retrying. |
| `cluster_exists` | That local name is taken. Use `--force`, or `--as` to pick another. |
| `config_error` | The local config couldn't be read or written. |
| `unknown` | A failure with no code. Treat it as uninterpretable. |

## Usage

```bash
miren cluster add [flags]
```

## Flags

- `--address, -a` — Address/hostname of the cluster (optional - will use from selected cluster)
- `--as` — Local name to store the cluster under, when it should differ from its name in Miren Cloud
- `--cluster, -c` — Name of the cluster to add, looked up in Miren Cloud unless --address is given (optional - will list available)
- `--force, -f` — Overwrite existing cluster configuration
- `--format` — Output format (text, json) (default: `text`)
- `--identity, -i` — Name of the identity to use (optional - will use the only one if single)
- `--json` — Shorthand for --format json
- `--organization` — Organization the named cluster belongs to, for when the same name exists in more than one
- `--via-cloud` — Reach the cluster through Miren Cloud instead of dialing it, for a cluster this machine has no route to

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Add a cluster interactively:**

```bash
miren cluster add
```

**Add a cluster by name, without the picker:**

```bash
miren cluster add --cluster my-cluster
```

**Add a cluster by name under a different local name:**

```bash
miren cluster add --cluster my-cluster --as staging
```

**Add a cluster, reporting the result (and any failure) as JSON:**

```bash
miren cluster add --cluster my-cluster --format json
```

**Add a cluster with a specific address:**

```bash
miren cluster add --cluster my-cluster --address 10.0.0.1:8443
```

## See also

- [`miren cluster`](/command/cluster)
