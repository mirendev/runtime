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

An upgrade from Cloud runs the same operation as `miren upgrade` on the host. The server downloads the release, snapshots its data, installs the release, and restarts. It counts as done only when the new server reports ready on the new version. If that doesn't happen, the server puts the previous version and its data back on its own.

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

Automatic upgrades are off until you turn them on, and we recommend turning them on. While they're off, the **Updates** tile says so and offers **Turn on**, which opens the settings in the **Automatic upgrades** row below the tiles. For a cluster nobody has set up, the form opens with the **Stable** channel and a window from 02:00 to 05:00 every day in your browser's timezone. Check the window, move it if it doesn't suit the cluster, and save. Once they're on, the Updates tile shows when the next window opens, while it's closed.

Cloud doesn't turn them on for you because an upgrade is downtime. The server restarts, and while it's down your apps keep running but requests to them fail. That usually lasts a minute or two, and longer if the upgrade fails and rolls back. Only you know when that's least disruptive.

Automatic upgrades are set per cluster. Turning them on for one cluster leaves the organization's other clusters alone, so you can give staging and production different channels or windows.

### Channels

| Channel | What it installs |
| --- | --- |
| **Stable** (recommended) | A release once it has proven out. |
| **Latest** | Every release, on the first window after it ships. |

A release reaches **stable** once it has been the latest release for seven days without a newer release replacing it. A release replaced within the week never becomes stable, however old it gets: whatever replaced it was released for a reason. If the latest release is ever rolled back, releases above where it went back to are skipped too. Miren Cloud works this out from the history of the latest channel, so stable always names a release that was once latest and held there.

Under this rule, a release reaches stable at least a week after it ships. A week with several quick fix releases can hold stable back longer, until one of them lasts seven days.

### Choosing a window

The **maintenance window** is when Cloud may start an upgrade: which days, what time it opens, and how long it stays open, in a timezone you pick. A window that opens late in the evening and runs past midnight belongs to the day it opens.

Pick the hours when the apps on the cluster have the least traffic, in the timezone of the people using them. Don't go by the server's clock, which is usually UTC. 02:00 UTC is the middle of the night in Europe and early evening on the US West Coast. If your traffic follows a working week, a weekend window avoids weekday users entirely.

Give the window room. Cloud checks every few minutes and starts an upgrade only while the window is open, and an upgrade takes a few minutes, longer with [distributed runners](../distributed-runners.md). A window of an hour or more is comfortable.

Cloud starts at most one upgrade per release. If an automatic upgrade fails, Cloud doesn't try that release again; retry it with the upgrade button, or wait for the next release.

## Notifications

Organization admins can send upgrade outcomes to Slack from the **Notifications** section of the organization page. Add a channel with a Slack incoming webhook URL and use **Send test** to check it. Each finished operation posts one message: the version change on success, or the error and whether the cluster rolled back.
