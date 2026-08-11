---
title: Running Miren on a Tailnet
description: How to run a Miren cluster on a host with no public address, using Tailscale or another overlay network.
keywords: [tailscale, tailnet, vpn, wireguard, private network, overlay, headscale, dns-01, private cluster]
---

# Running Miren on a Tailnet

Your cluster doesn't need a public IP. A box on your home network, a VPS with
every inbound port closed, or a machine that only answers on
[Tailscale](https://tailscale.com/) all make perfectly good clusters. Most of
it just works, but two things change when nothing on the internet can reach
you: how your apps get certificates, and how visitors reach them.

This page covers both, plus what to expect from the dashboard.

:::info[Applies to any overlay]
Tailscale is the common case, but nothing here is specific to it. Headscale,
Nebula, ZeroTier, and plain WireGuard all behave the same way.
:::

## Minimum working example

Install and register the cluster the way you normally would. The one required
change on a host the internet can't reach is switching app certificates to a
DNS-01 challenge:

```toml title="/etc/miren/server.toml"
[tls]
acme_email = 'you@example.com'
acme_dns_provider = 'dnsimple'
```

Put your DNS provider's API credentials in `/var/lib/miren/server/env` and
restart the server. Point your domain's DNS at the tailnet address (or use
[Miren Anywhere](/miren-cloud/miren-anywhere) for public traffic), and deploys
work unchanged.

## Setting up

Install Miren and register the cluster the way you normally would. There's no
special configuration for this part.

At startup your server looks at its network interfaces and reports the
addresses a client might reach it on, tailnet addresses included. When you run
`miren cluster add` from another machine on the tailnet, the CLI tries every
address it got back, in parallel, and keeps whichever answers first. Your
tailnet address is in there, so it connects.

Certificates for the API are handled for you. Miren signs them with the
cluster's own CA and includes every address it found, so connecting over the
tailnet verifies without any extra setup.

## Certificates for your apps

This is the part that needs a decision from you.

Miren gets certificates for your apps from Let's Encrypt, and the default
challenge type asks Let's Encrypt to connect to your host on port 80. On a
tailnet-only box that connection can't happen, so the certificate never
arrives.

Use a DNS-01 challenge instead. It proves you own the domain by writing a DNS
record, so nothing ever needs to reach your host:

```toml
[tls]
acme_email = 'you@example.com'
acme_dns_provider = 'dnsimple'
```

Put your provider's API credentials in the server environment file at
`/var/lib/miren/server/env`, then restart. [TLS](/tls) has the full list of
supported providers and the variables each one expects.

:::warning[HTTP-01 can't work here]
This isn't a preference, it's the only option. An HTTP-01 challenge needs Let's
Encrypt to reach port 80 on your host from the public internet, so on a
tailnet-only cluster it fails every time with a validation timeout.
:::

## Getting traffic to your apps

Who needs to reach your apps decides this one.

**Just you and your tailnet?** Point DNS at your tailnet address. A `100.x`
address in public DNS is fine and fairly common: it resolves for everyone but
only routes for people on your tailnet. Tailscale's
[MagicDNS](https://tailscale.com/kb/1081/magicdns/) works too if you'd rather
not publish anything at all.

**The whole internet?** Use [Miren Anywhere](/miren-cloud/miren-anywhere). It
carries app traffic through the Miren POP network and reaches your cluster over
the connection your cluster already makes outbound, so you don't need a public
address or any open ports. On a cluster with no public address, it turns on by
default.

:::note[Deploys still go direct]
Miren Anywhere carries app traffic only. Deploys, logs, and everything else the
CLI does talk straight to your cluster on UDP 8443, so you deploy from a machine
on the tailnet.
:::

## What the dashboard shows

A tailnet-only cluster reads **Online**, with the control plane **not
reachable** and apps reachable through Miren Anywhere. Only the middle one looks
alarming, and it isn't: it means Miren Cloud can't open a connection from the
public internet, which is exactly what you set up. See
[Cluster Connectivity](/miren-cloud/connectivity) for what each of the three
checks actually measures.

## Troubleshooting

**Start with `miren debug advertise`.** Run it on the host and it prints every
address your server found, along with why it kept or dropped each one. That's
usually faster than guessing:

```bash
sudo miren debug advertise
```

Addresses on container bridges like `docker0` and Miren's own `rt0` get dropped
on purpose, since they only ever route to workloads on the same host.

**Pin an address by hand** when discovery gets it wrong, like a host behind a
static NAT. Anything in `additional_ips` skips discovery entirely and is always
advertised:

```toml
[tls]
additional_ips = ["100.64.0.10"]
```

Advertisement is worked out at startup, so restart the server after changing
it.

**If connections time out,** first make sure you're on a current release.
Miren v0.12.1 and earlier couldn't complete a QUIC handshake through a tunnel
at all, which made tailnets unusable. After that, check the server is actually
listening with `sudo ss -lunp | grep 8443`, and remember that Tailscale ACLs
are enforced by `tailscaled` itself, so a permissive host firewall won't help
if an ACL is blocking the port.
