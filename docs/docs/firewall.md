---
sidebar_position: 5
---

# Firewall Configuration

:::info
Miren automatically configures firewall rules during setup. Most users won't need to think about firewalls at all. This page is primarily useful for troubleshooting networking issues or understanding what Miren does under the hood.
:::

Miren automatically configures iptables rules to enable container networking. This page explains how Miren's firewall rules work and how to troubleshoot networking issues.

## How Miren Configures Firewall Rules

When Miren sets up the network bridge, it installs iptables rules in two chains:

### FORWARD Chain

The FORWARD chain controls traffic passing through the host (container-to-container and container-to-internet traffic). Miren adds rules to accept all traffic to and from the bridge interface:

```
-A FORWARD -i miren0 -j ACCEPT
-A FORWARD -o miren0 -j ACCEPT
```

### INPUT Chain

The INPUT chain controls traffic destined for the host itself. Miren adds rules to allow containers to reach host services:

| Port | Protocol | Purpose |
|------|----------|---------|
| 53   | UDP/TCP  | DNS resolution (containers query host DNS) |
| 5000 | TCP      | Local container registry (buildkit pushes images here) |

## Rule Ordering

Miren inserts rules at position 1 (the beginning of each chain) to ensure they take precedence over any existing restrictive rules. This is important because some cloud providers ship default firewall configurations with blanket REJECT rules.

For example, Oracle Cloud Ubuntu images have a REJECT rule at the start of the FORWARD chain by default. If Miren appended its ACCEPT rules to the end, they would never be evaluated.

## Required External Ports

If you're running Miren on a cloud provider, you'll need to configure security groups or network ACLs to allow external traffic to reach the Miren server.

### Inbound Ports

| Port | Protocol | Purpose | Required |
|------|----------|---------|----------|
| 8443 | UDP | Miren API (QUIC) - CLI and client connections | Yes |
| 80 | TCP | HTTP traffic to your applications (redirects to HTTPS) | Yes |
| 443 | TCP | HTTPS traffic to your applications | Yes |

**Miren API (UDP 8443):** The Miren API uses QUIC (HTTP/3) over UDP. This is how the CLI communicates with the server and how remote clients connect to your cluster.

**HTTP Ingress (TCP 80/443):** Application traffic uses standard HTTP/HTTPS. Port 80 handles ACME certificate challenges and redirects to HTTPS. Port 443 serves your applications over TLS.

### Outbound Connectivity

Miren requires outbound internet access for several operations. Most cloud providers allow all outbound traffic by default, but if you have restrictive egress rules, ensure the following destinations are reachable:

| Destination | Port | Purpose |
|-------------|------|---------|
| oci.miren.cloud | 443 | Miren's container registry (base images for builds) |
| api.miren.cloud | 443 | Miren Cloud API (authentication, cluster registration) |
| registry-1.docker.io | 443 | Docker Hub (if your app references Docker Hub images) |
| Package registries | 443 | Language-specific package managers (see below) |
| Let's Encrypt | 80/443 | ACME certificate issuance |

**During builds**, Miren pulls base images from `oci.miren.cloud` for supported language stacks (Python, Ruby, Node.js, Go, Bun). If your application references other container images, those registries must also be reachable.

**Package managers** used during builds need access to their respective registries:

- **Ruby**: rubygems.org
- **Python**: pypi.org
- **Node.js/Bun**: registry.npmjs.org
- **Go**: proxy.golang.org (and any private module sources)
- **System packages**: debian/ubuntu apt repositories, Alpine apk repositories

**Miren Cloud** connectivity is required for authentication (`miren login`) and cluster registration (`miren server install`, `miren server docker install`, `miren server register`). If you're running Miren in standalone mode without cloud features, this isn't required.

## Cloud Provider Considerations

### Oracle Cloud

Oracle Cloud instances come with restrictive default iptables rules. Miren handles this automatically by inserting rules at position 1, but be aware that:

- The default FORWARD chain has a REJECT rule that blocks all forwarding
- The default INPUT chain may not allow traffic from container networks
- You must also configure the VCN Security List to allow inbound traffic (see above)

### AWS

AWS security groups operate at the network layer outside the instance. Miren's iptables rules handle traffic inside the instance, but you still need to configure security groups to allow external traffic to reach your services (see above).

### Other Providers

Most cloud providers have some form of default firewall. Miren's rule insertion strategy should work out of the box, but if you encounter networking issues, check for conflicting rules. Remember to configure both:

1. **Cloud-level firewalls** (security groups, network ACLs) - allow external traffic to reach the instance
2. **Host-level firewalls** (iptables) - Miren handles this automatically

## Troubleshooting

### Symptoms of Firewall Issues

- "no route to host" errors during deploys
- Containers unable to resolve DNS
- Buildkit failing to push to local registry
- Intermittent network failures from sandboxes

### Diagnostic Commands

Check current iptables rules:

```bash
# View FORWARD chain
sudo iptables -L FORWARD -n -v --line-numbers

# View INPUT chain
sudo iptables -L INPUT -n -v --line-numbers

# Look for Miren's bridge interface rules (usually miren0)
sudo iptables -L -n -v | grep miren
```

Check if the bridge interface exists:

```bash
ip link show miren0
```

Verify containers can reach the host:

```bash
# From inside a sandbox, try to reach host DNS
ping 10.8.0.1  # (adjust IP to your bridge gateway)
```

### Common Issues

**Rules inserted in wrong order**: If you see REJECT rules before Miren's ACCEPT rules, the firewall may have been configured after Miren started. Restart Miren to re-insert rules at position 1.

**IPv6 issues**: Miren configures both IPv4 and IPv6 rules. If your environment doesn't support IPv6, you may see errors during bridge setup. This is usually harmless if IPv4 works.

**Conflicting firewall managers**: Tools like `ufw`, `firewalld`, or cloud-specific agents may conflict with Miren's iptables rules. If possible, configure these tools to allow Miren's traffic or disable them in favor of direct iptables management.
