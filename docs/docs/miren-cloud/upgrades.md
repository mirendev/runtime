---
title: Upgrades from Miren Cloud
description: Upgrade a cluster from its page in Miren Cloud, keep it current automatically on a schedule you pick, and get told in Slack how it went.
keywords: [upgrade, restart, automatic upgrades, scheduled upgrades, maintenance window, release channel, stable, rollback, notifications, slack]
---

import CliCommand from '@site/src/components/CliCommand';

# Upgrades from Miren Cloud

A registered cluster's page in Miren Cloud has a **Server** section. Its tiles show which version the cluster runs, whether a newer release is out, and how the last change went, and the upgrade button sits in the section's header. From there an organization admin can upgrade the cluster, and optionally let Miren Cloud keep it current on a schedule.

## Minimum working example

1. Open the cluster in Miren Cloud. In the **Server** section, the **Updates** tile says when a newer release is available.
2. Click the **Upgrade to** button in the section's header, which names the new version.
3. Watch the progress below the tiles. The cluster disconnects while its server restarts and reconnects on the new version.

## What an upgrade does

An upgrade from Cloud runs the same operation as `sudo miren upgrade` on the host. The server downloads the release, snapshots its data, installs the release, and restarts. It counts as done only when the new server reports ready on the new version. If that doesn't happen, the server puts the previous version and its data back on its own.

Once the server is on the new version, it upgrades any [distributed runners](../distributed-runners.md) one at a time. If the first runner fails, the rest are left alone.

The operation runs on the cluster, not in your browser or in Cloud. Progress on the page follows the cluster's own record of it, so closing or reloading the page is safe. Restarts and upgrades started on the host with `miren upgrade` show up in the same history.

## Requirements

- The cluster is [registered](./overview.md) with Miren Cloud and currently connected.
- It runs Miren v0.16.0 or later. Earlier releases can't take upgrades from Cloud, so upgrade those once on the host:

  <CliCommand context="server">
  ```miren
  sudo miren upgrade
  ```
  </CliCommand>

  Each distributed runner needs the same one-time upgrade on its own host before the server can upgrade it for you.

## Automatic upgrades

Automatic upgrades are off until you turn them on. The **Automatic upgrades** row in the Server section takes a release channel and a maintenance window.

| Channel | What it installs |
| --- | --- |
| **Stable** (recommended) | A release once it has proven out, usually about a week after it ships. |
| **Latest** | Every release, on the first window after it ships. |

The **maintenance window** is when Cloud may start an upgrade: which days, what time it opens, and how long it stays open, in a timezone you pick. It defaults to 02:00 to 05:00 every day. Cloud starts at most one upgrade per release. If an automatic upgrade fails, Cloud doesn't try that release again; retry it with the upgrade button, or wait for the next release.

## Notifications

Organization admins can send upgrade outcomes to Slack from the **Notifications** section of the organization page. Add a channel with a Slack incoming webhook URL and use **Send test** to check it. Each finished operation posts one message: the version change on success, or the error and whether the cluster rolled back.
