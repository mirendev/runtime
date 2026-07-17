---
title: Miren Cloud
description: Connect your self-hosted Miren clusters to a public front door, team access, and multi-environment workflows, without giving up ownership of your infrastructure.
keywords: [miren cloud, control plane, subdomains, miren anywhere, teams, multi-environment]
---

# Miren Cloud

You install Miren on your own server and you own the whole thing. Run it standalone and it works great on its own, but it's an island: your apps are only reachable where your network reaches, backups are yours to arrange, and it's just you managing it.

Miren Cloud is the connective tissue you can opt into. It doesn't take over your infrastructure. It sits alongside your clusters and adds the things that are a pain to build yourself.

## What you get by connecting

**A public front door for your apps.** Claim a [subdomain](/miren-cloud/subdomains) like `mycluster.run.garden` and your apps get a real address. With [Miren Anywhere](/miren-cloud/miren-anywhere), they stay reachable from the internet even when your cluster has no public IP, so a home lab or a box behind NAT can serve real traffic.

**Your team.** Bring other people in and control who can do what with role-based access, instead of sharing one set of credentials.

**Multiple environments.** Run separate clusters for production, staging, and preview work under one account, and switch between them from the CLI.

## You still own everything

Connecting to Miren Cloud is additive, not lock-in. Miren runs fully standalone, and if you'd rather keep it that way you can install with cloud turned off:

```bash
sudo miren server install --without-cloud
```

A standalone cluster reaches the network however you've wired it, and none of the cloud features above apply. You can always connect later.

## Getting connected

When you run `miren server install`, Miren registers the cluster with Miren Cloud and walks you through creating your account. To connect a server you installed standalone, register it after the fact:

```bash
miren server register -n my-cluster
```

Then log in from whatever machine you work on:

```bash
miren login
```

The [Getting Started](/getting-started) guide covers the full install-and-first-deploy flow. For everything the CLI can do with clusters, see the [`miren cluster`](/command/cluster) reference.

## Explore

- [Subdomains](/miren-cloud/subdomains) — Claim a hostname like `mycluster.run.garden` for your apps
- [Miren Anywhere](/miren-cloud/miren-anywhere) — Serve public apps from a cluster with no public IP
- [Cluster Connectivity](/miren-cloud/connectivity) — Tell whether your cluster is online, deployable, and serving apps
- [Cloud Updates](/miren-cloud/cloud-updates) — What's new in Miren Cloud
