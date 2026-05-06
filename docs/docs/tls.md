---
title: TLS Certificates
description: Automatic TLS certificate provisioning via Let's Encrypt, including DNS challenges and custom certificates.
keywords: [tls, ssl, certificates, https, lets encrypt, acme, dns challenge]
---

import CliCommand from '@site/src/components/CliCommand';

# TLS Certificates

Miren automatically provisions TLS certificates for your applications using [Let's Encrypt](https://letsencrypt.org/). By default, no configuration is needed — when you set a route for your app, Miren obtains a certificate automatically.

## How It Works

When a request arrives for a hostname with a configured route, Miren provisions a TLS certificate from Let's Encrypt using the ACME protocol. Certificates are cached on disk and renewed automatically before they expire.

Hostnames without a configured route are served with a self-signed fallback certificate (browsers will show a warning).

For wildcard routes (e.g., `*.myapp.example.com`), TLS certificates are provisioned for each matching subdomain as requests arrive. See [Wildcard Routes](/traffic-routing#wildcard-routes) for details.

## Challenge Types

Let's Encrypt needs to verify that you control the domain before issuing a certificate. Miren supports two verification methods.

### HTTP-01 Challenge (Default)

The default method. Let's Encrypt makes an HTTP request to your server on port 80 to verify domain ownership.

**Requirements:**
- Your server must be reachable from the public internet on ports 80 and 443
- DNS for your domain must point to the server's public IP

This is the zero-configuration option — it works out of the box with `miren server install`.

### DNS-01 Challenge

If your server isn't publicly reachable (e.g., it's behind a VPN like Tailscale, on a private network, or doesn't have ports 80/443 open to the internet), you can use DNS-01 challenges instead. This method proves domain ownership by creating a DNS TXT record, so it works regardless of whether your server is reachable from the internet.

Miren uses [lego](https://go-acme.github.io/lego/) for DNS challenges, which supports [90+ DNS providers](https://go-acme.github.io/lego/dns/).

#### Configuration

Add the DNS provider and ACME email to your server config file:

```toml title="/var/lib/miren/server/config.toml"
[tls]
acme_dns_provider = "dnsimple"
acme_email = "you@example.com"
```

Each DNS provider requires credentials passed via environment variables. Create an environment file for the Miren service:

```bash title="/var/lib/miren/server/env"
DNSIMPLE_OAUTH_TOKEN=your-token-here
```

Then update the systemd service to load the environment file and config:

<CliCommand context="server">
```bash
sudo systemctl edit miren --force
```
</CliCommand>

Add the environment file:

```ini
[Service]
EnvironmentFile=/var/lib/miren/server/env
```

Restart to pick up the changes:

<CliCommand context="server">
```bash
sudo systemctl restart miren
```
</CliCommand>

#### Provider Examples

Each provider needs different environment variables. Here are a few common ones:

**DNSimple:**
```toml
acme_dns_provider = "dnsimple"
```
```bash
DNSIMPLE_OAUTH_TOKEN=your-token
```

**Cloudflare:**
```toml
acme_dns_provider = "cloudflare"
```
```bash
CF_DNS_API_TOKEN=your-token
```

**Route 53 (AWS):**
```toml
acme_dns_provider = "route53"
```
```bash
AWS_ACCESS_KEY_ID=your-key
AWS_SECRET_ACCESS_KEY=your-secret
AWS_REGION=us-east-1
```

See the [lego DNS provider documentation](https://go-acme.github.io/lego/dns/) for the full list of supported providers and their required environment variables.

## Server Configuration Reference

All TLS settings live under the `[tls]` section of the server config file (typically `/var/lib/miren/server/config.toml`):

| Setting | CLI Flag | Description |
|---------|----------|-------------|
| `acme_email` | `--acme-email` | Email for Let's Encrypt account registration and expiry notifications |
| `acme_dns_provider` | `--acme-dns-provider` | DNS provider name for DNS-01 challenges (e.g., `cloudflare`, `route53`, `dnsimple`) |
| `standard_tls` | `--serve-tls` | Enable TLS on ports 443/80 (default: `true`) |

To bind the HTTPS ingress to a non-default address, see [`ingress.https_address`](/server-config#ingress).

## Troubleshooting

### Certificate Not Provisioning

Check the server logs for ACME errors:

<CliCommand context="server">
```bash
sudo journalctl -u miren | grep -i acme
```
</CliCommand>

**HTTP-01 challenges:** Ensure ports 80 and 443 are reachable from the public internet. See [Firewall Configuration](/firewall) for cloud provider-specific guidance.

**DNS-01 challenges:** Verify your DNS provider credentials are correct and the environment file is loaded:

<CliCommand context="server">
```bash
sudo systemctl show miren | grep EnvironmentFile
```
</CliCommand>

### Wrong Certificate / Self-Signed Warning

If you're seeing a self-signed certificate warning for a domain that should have a real certificate, check that a route is configured for that hostname:

<CliCommand context="client">
```miren
miren route
```
</CliCommand>

Miren only provisions ACME certificates for hostnames with explicitly configured routes. All other hostnames get the self-signed fallback.
