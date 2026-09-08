---
title: Miren Club
description: Miren Club is our shared cloud cluster for demos and experiments — request access in Discord, connect your CLI, and deploy to *.miren.club.
keywords: [miren club, demo cluster, shared cluster, hosted, discord, experiment]
---

import CliCommand from '@site/src/components/CliCommand';

# Miren Club

Miren Club is our shared cloud cluster — a place to experiment with Miren and deploy something to the real internet without setting up a server of your own first. It's the fastest way to go from "I've heard of Miren" to "my app is live," and a great sandbox for demos, hack projects, and kicking the tires.

If you'd rather run Miren on your own machine, that's the [Getting Started](./getting-started.md) path. Miren Club is for when you just want a cluster to already exist.

## Requesting Access

Access is granted through our [Discord](https://miren.dev/discord). Here's the flow:

1. Sign in to [Miren Cloud](https://miren.cloud) with your GitHub or Google account.
2. Post in the **#miren-club** channel asking for access, with a sentence about what you'd like to deploy.
3. One of us will DM you an invite link.

Once you've accepted the invite, you're a member of the Miren Club organization in Miren Cloud and ready to connect.

## Connecting Your CLI

With access sorted, log in from your local machine:

<CliCommand context="client">
```miren
miren login
```
</CliCommand>

Follow the prompts to authenticate with Miren Cloud. Then bind the club cluster to your local CLI:

<CliCommand context="client">
```miren
miren cluster add

Select a cluster to bind:
   NAME   ORGANIZATION   ADDRESS
▸  club   Miren Club     34.27.122.56:8443 (+6)
```
</CliCommand>

Pick **club** and you're pointed at the shared cluster. Every `miren` command you run now targets Miren Club until you [switch clusters](./command/cluster.md).

## Deploying and Going Live

Deploying to Miren Club works exactly like deploying anywhere else. From your project directory:

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

The one perk that comes for free: the club owns the `miren.club` domain with wildcard DNS, so you can route your app to any name you like under it. Pick something and set the route:

<CliCommand context="client">
```miren
miren route set whateveryoulike.miren.club myapp
```
</CliCommand>

That's it — `whateveryoulike.miren.club` is live on the internet, TLS and all. See [Traffic Routing](./traffic-routing.md) for more on routes.

## Club Rules

Miren Club is a shared resource, so a few ground rules keep it pleasant for everyone:

- **Don't hog the GBs or the MHz.** Other people are sharing this cluster with you. Be a considerate neighbor with memory and CPU.
- **Watch your step.** Right now there's a single namespace and no per-app permissions, so it's possible to clobber someone else's stuff. Deploy carefully and stay in your lane.
- **Have fun, but be responsible.** Anything that violates our [Code of Conduct](./conduct.md) gets removed, and you'll have to turn in your Miren Club membership card.

## Need Help?

- Questions about **Miren Club specifically** (access, the shared cluster, routing to `miren.club`)? Ask in **#miren-club** on [Discord](https://miren.dev/discord).
- Questions about **using Miren generally**? Ask in **#feedback**, or check [Troubleshooting](./troubleshooting.md).

## Next Steps

- [Getting Started](./getting-started.md) — The full deploy walkthrough, which works the same on Miren Club
- [Traffic Routing](./traffic-routing.md) — Custom domains, wildcards, and path-based routing
- [App Configuration](./app-configuration.md) — Configure your app with `.miren/app.toml`
