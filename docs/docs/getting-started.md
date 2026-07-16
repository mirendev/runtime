---
title: Getting Started
description: Install Miren, set up a server, and deploy your first app in minutes.
keywords: [getting started, install, setup, quickstart, first deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Getting Started

Get up and running with Miren in minutes.

## Two Contexts: Server and Client

Miren runs in two places. The **server** is the Linux machine that hosts your applications. The **client** is wherever you work: your laptop, a CI runner, or even the server itself. Most `miren` commands are client commands that talk to a remote server, but a few (like `miren server install`) run directly on the server to set it up.

Throughout these docs, CLI examples are labeled <span class="cli-command__badge cli-command__badge--server">SERVER</span> or <span class="cli-command__badge cli-command__badge--client">CLIENT</span> so you always know which machine a command belongs on. When both labels appear, the command works in either context.

## Installation

Install Miren on both your server and your local machine. Head to [miren.dev/get-started](https://miren.dev/get-started) for platform-specific instructions and options.

Once installed, verify it's working on either machine:

<CliCommand context="both">
```miren
miren version
```
</CliCommand>

### System Requirements (Server)

- **Operating System**: Linux (kernel 5.10+)
- **Architecture**: x86_64 or arm64
- **Memory**: 4GB minimum, 8GB recommended
- **Storage**: 50GB minimum, 100GB recommended

See [System Requirements](/system-requirements) for details on why these numbers matter.

## Set Up Your Server

(Skip this section if you are using our [demo cluster](#using-our-demo-cluster))

On your server, run the install command to set up the Miren runtime:

<CliCommand context="server">
```miren
sudo miren server install
```
</CliCommand>

This will download required components, register your cluster with [miren.cloud](/miren-cloud/overview) (follow the prompts), install and start the Miren systemd service, and configure the local CLI to talk to it.

To skip cloud registration and run standalone:

<CliCommand context="server">
```miren
sudo miren server install --without-cloud
```
</CliCommand>

### Using Our Demo Cluster

Ask for access to our demo cluster, [Miren Club](/miren-club), in #miren-club on [Discord](https://miren.dev/discord). Once you have access, log in and add the cluster:

<CliCommand context="client">
```miren
miren login
```
</CliCommand>

Follow the prompts to authenticate with Miren Cloud, then bind the cluster to your local CLI:

<CliCommand context="client">
```miren
miren cluster add

Select a cluster to bind:
   NAME   ORGANIZATION   ADDRESS
▸  club   Miren Club     34.27.122.56:8443 (+6)
```
</CliCommand>

## Deploy Your First App

Let's deploy a real app so you can see the full flow. Clone the [sample apps repo](https://github.com/mirendev/sample-apps) and navigate to the demo app:

<CliCommand context="client">
```bash
git clone https://github.com/mirendev/sample-apps.git
cd sample-apps/demo
```
</CliCommand>

This is a small Bun web app that's already configured for Miren. Before deploying, you can preview what Miren detects:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

This shows the detected stack, services, and how each service will be started. When you're ready, deploy it:

<CliCommand context="client">
```miren
miren deploy

  ✓ Deploying: demo → club
  ✓ Upload artifacts (0.1s) - 13.4 KB at 180.5 KB/s
  ✓ Build & push image (7.8s) - 9 steps completed

Updated version demo-vCZpcrAAaU6mULzMSSBwc4 deployed. All traffic moved to new version.

No routes configured for this app.
To set a hostname, try: miren route set demo.cluster-jwomf2l0tn8z.miren.systems demo
```
</CliCommand>

Your app is deployed and running! Miren suggests a hostname on your cluster's `.miren.systems` subdomain. If your cluster is publicly accessible, go ahead and set that route:

<CliCommand context="client">
```miren
miren route set demo.cluster-jwomf2l0tn8z.miren.systems demo
```
</CliCommand>

Then open the URL in your browser to see the demo app.

![The demo app running in a browser](/img/demo-app-browser.png)

:::note[Not publicly accessible?]
If your server is behind a firewall or on a private network, `miren route set` still configures the route. To serve your apps to the internet without a public IP, [Miren Anywhere](/miren-cloud/miren-anywhere) routes traffic through Miren Cloud.
:::

Otherwise, verify the app is running locally with `miren app list` and `miren logs`, and see [Firewall](/firewall) for making the cluster directly reachable.

## See It Running

Check that your app deployed successfully:

<CliCommand context="client">
```miren
miren app list

  NAME   VERSION                        SCALE   ROUTE
  demo   demo-vCZpcrAAaU6mULzMSSBwc4    1       demo.cluster-jwomf2l0tn8z.miren.systems
```
</CliCommand>

You can also tail the logs to see requests coming in:

<CliCommand context="client">
```miren
miren logs

S 2026-04-07 10:08:12: Server running at http://localhost:3000
S 2026-04-07 10:08:15: GET / 200 2ms
S 2026-04-07 10:08:15: GET /images/Miren-Logo-Secondary.svg 200 1ms
```
</CliCommand>

That's it! You have an app running on Miren.

## Next Steps

Now that you've got something deployed, here's where to go depending on what you need.

**Deploy your own app.** Miren auto-detects Python, Node, Bun, Go, Ruby, and Rust projects. Run `miren init` in your project to create a [`.miren/app.toml`](/app-configuration) config, then `miren deploy`. You can always provide a `Dockerfile` if you need full control over the build. For a step-by-step walkthrough for your language, see the [Language Guides](/guides).

**Manage multiple clusters.** If you have more than one server, use [`miren cluster`](/command/cluster) to list your clusters and `miren cluster switch` to change which one you're targeting before deploying.

**Configure your app.** The [App Configuration](/app-configuration) guide covers `.miren/app.toml` in depth: setting commands, ports, environment variables, concurrency, and more.

**Set up routes.** Your first app gets a default route, but additional apps need explicit routing. See [Routes](/traffic-routing) for custom domains and path-based routing.

**Scale your app.** Miren autoscales by default (like Cloud Run), spinning instances up and down with traffic. If you need fixed instance counts for things like databases or workers, see [Application Scaling](/scaling).

**Add persistent storage.** [Disks](/disks) let you attach storage volumes to services, with built-in backup and restore.

**Explore the CLI.** The full [CLI Reference](/commands) documents every command and flag.

**Get help.** If something isn't working, check [Troubleshooting](/troubleshooting) or ask in #miren-club on [Discord](https://miren.dev/discord).
