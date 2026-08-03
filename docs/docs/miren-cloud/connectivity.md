---
title: Cluster Connectivity
description: How to read the Connectivity panel — whether your cluster is online, whether Miren can deploy to it, and whether users can reach your apps.
keywords: [connectivity, reachability, control plane, netcheck, quic, firewall, miren anywhere, unreachable]
---

# Cluster Connectivity

The **Connectivity** panel on a cluster's page answers three separate questions. They're easy to lump together, but they fail independently and they have different fixes, so Miren keeps them apart:

1. [Is the cluster online?](#is-the-cluster-online) — is it alive and checking in with Miren Cloud at all?
2. [Can Miren deploy to this cluster?](#can-miren-deploy-to-this-cluster) — can the CLI and the deploy path reach it over the internet?
3. [Can users reach your apps?](#can-users-reach-your-apps) — can visitors load the applications running on it?

A cluster can be online but not deployable, or undeployable over the internet yet still serving apps through the Miren POP network. Reading the three checks separately tells you exactly which layer is having trouble.

:::info[What this page covers]
This page explains what each check means and how to fix a "not reachable" verdict. For the exact ports and provider-specific firewall setup, see [Firewall Configuration](/firewall).
:::

## Is the cluster online?

This check is about liveness, not ports. Your Miren server checks in with Miren Cloud on a short interval; if Cloud has heard from it recently, the cluster reads as **Online**, and if there's also a live control link it reads as **Connected**. If check-ins stop, it goes **Offline**.

A cluster being online only means it's running and can talk *out* to Miren Cloud. It says nothing about whether anything can reach it from the outside — that's the next two checks.

:::note[Online but nothing else works?]
Outbound connectivity (the cluster reaching Cloud) and inbound connectivity (clients reaching the cluster) are different paths. A firewall commonly allows the first while blocking the second, which is exactly the case the next check catches.
:::

## Can Miren deploy to this cluster?

This is the **control plane**: the Miren API that the CLI and the deploy path use, served over QUIC on UDP port 8443. For `miren deploy`, `miren logs`, and everything else the CLI does to reach your cluster over the internet, this port has to be reachable.

When Miren can't reach it, the panel doesn't just say "Not reachable" — it names the culprit:

```text
Miren Cloud found this cluster at 203.0.113.10, but couldn't open a connection to it on UDP 8443 (QUIC).
```

That verdict comes from a reachability check Miren Cloud runs against the address your cluster reports (see [How Miren checks reachability](#how-miren-checks-reachability)). Cloud can see your public address but can't open a connection back to it on the port the control plane needs. Something in the inbound path is dropping UDP 8443 — most often a host firewall, a cloud-provider security group, or a NAT with no port forward for it.

To fix it, make sure inbound **UDP 8443** reaches your server end to end — through the host firewall, any cloud security group, and any NAT in front of it. The cluster becomes reachable the moment the port opens; the server's QUIC listener is already running, so nothing needs restarting. The dashboard catches up on its next reachability check (see the note below), or right away if you restart to force one:

```bash
sudo miren server restart
```

If your cluster's public address isn't the one Miren discovered — for example it sits behind a load balancer or a static NAT — set the reachable address explicitly with `additional_ips` in your [server configuration](/server-config) instead of relying on discovery.

:::tip[Reachable, but no public address]
A cluster on a private network (home lab, VPC with no public IP, or a host that only answers on a tailnet) will read "Not reachable" here and that's expected — you deploy to it from the same LAN or overlay. Miren Anywhere carries app traffic, not the control plane, and so **won't** change this check. For the tailnet case end to end, see [Running Miren on a Tailnet](/tailscale).
:::

## Can users reach your apps?

This check is about **application traffic** — the HTTP/HTTPS your visitors load, served on ports 80 and 443, entirely separate from the control-plane port above.

There are two ways users can reach your apps:

- **Directly**, when the cluster has a public address. The apps are served straight from your server.
- **Via Miren Anywhere**, when it doesn't. Miren routes app traffic through the Miren POP network, so your apps stay reachable even from behind NAT with no public address of their own.

This is why a cluster can show "Not reachable" for the control plane and still show apps as available: Miren Anywhere solves the app-traffic problem without solving the deploy problem. See [Custom subdomains](/miren-cloud/subdomains) for how app hostnames are provisioned.

## How Miren checks reachability

Your Miren server reports the addresses it believes it's reachable at, and Miren Cloud runs a **netcheck**: from the outside, it looks at the public address the connection actually came from and probes the ports your server advertised. The result is what the Control plane check reports.

Three outcomes are possible:

- **Reachable** — Cloud connected to at least one advertised port. The address is confirmed and handed to clients.
- **Not reachable, address known** — Cloud saw a public address but couldn't connect to any port. Something in the inbound path (a firewall, a cloud security group, or a NAT) is dropping the traffic, and this is the verdict that names the specific port.
- **No public address** — Cloud only ever saw a private or carrier-NAT source, so there's nothing to hand out. Common for home labs, locked-down VPCs, and tailnet-only hosts; expected, not an error.

To see which addresses your cluster settled on and why each one was kept or dropped, run `miren debug advertise` on the host. It reproduces the server's own logic and prints a reason per candidate, which is usually faster than inferring the answer from the dashboard.

:::note[The panel reflects the last check]
Reachability is re-checked periodically (roughly hourly) and at startup, not on every status report — so after you open a port the panel can lag even though the cluster is already reachable. Restart the server to force a fresh check, or wait for the next one.
:::
