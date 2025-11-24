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

## Cloud Provider Considerations

### Oracle Cloud

Oracle Cloud instances come with restrictive default iptables rules. Miren handles this automatically by inserting rules at position 1, but be aware that:

- The default FORWARD chain has a REJECT rule that blocks all forwarding
- The default INPUT chain may not allow traffic from container networks

### AWS

AWS security groups operate at the network layer outside the instance. Miren's iptables rules handle traffic inside the instance, but you still need to configure security groups to allow external traffic to reach your services.

### Other Providers

Most cloud providers have some form of default firewall. Miren's rule insertion strategy should work out of the box, but if you encounter networking issues, check for conflicting rules.

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
