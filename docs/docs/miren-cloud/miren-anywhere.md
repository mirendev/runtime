
# Miren Anywhere

Miren Anywhere is the connection between your cluster and Miren Cloud. It's how requests for your [subdomain](/miren-cloud/subdomains) reach your cluster, even when the cluster is behind NAT or a firewall.

## How It Works

When your cluster is registered with Miren Cloud, the miren server opens a long-lived connection to miren.cloud itself. When a request arrives at a Miren Cloud Point of Presence (POP), the POP determines which cluster the request is for and asks miren.cloud to have that cluster's server connect directly to the POP. Subsequent requests for your hostnames terminate at the POP, which forwards them to your cluster over the new connection.

The result: you don't need a public IP, port forwarding, or DNS gymnastics on your cluster — Miren Cloud handles ingress and uses Miren Anywhere as the path back to you.

## When It Runs

Miren Anywhere starts automatically whenever the miren server has cloud auth configured — i.e. after you've run `miren server install` (without `--without-cloud`) or `miren server register`. There's no flag to enable, no separate process to run.

:::info[Standalone clusters]
If you run a fully standalone cluster (`miren server install --without-cloud`), Miren Anywhere doesn't start; your cluster reaches the network however you've wired it.
:::

## How Requests Use Miren Anywhere

To use Miren Anywhere, requests need to arrive at the Miren Cloud POP network. There are two ways that can happen:

_These examples use `cluster-xyz` as a stand-in for cluster IDs. You can find cluster IDs in miren.cloud._

1. **A request to `cluster-xyz.global.miren.systems`.** `*.global.miren.systems` points at `pop-global.miren.cloud`, so any request to that hostname is routed explicitly to the cluster named in the hostname.
2. **A cluster whose port 443 is not publicly reachable.** When a cluster connects to miren.cloud it checks whether its own port 443 is publicly accessible. If it isn't, miren.cloud automatically points the cluster's default DNS record (`cluster-xyz.miren.systems`) at `pop-global.miren.cloud` so traffic flows through the POP network instead.

### Miren Cloud Subdomains

A subdomain assigned by miren.cloud automatically routes traffic via Miren Anywhere when the cluster has no publicly reachable port. It does this by pointing the subdomain at the cluster's auto-assigned DNS record — which, as described above, already points at `pop-global.miren.cloud`.

### User-Provided Domains

In the future, we'll allow users to bring their own domain names and route them through Miren Anywhere. Today, you can point any DNS record at a cluster's public IP, which works fine for clusters that are publicly reachable.

## Verifying the Connection

Once the server is running, look for Miren Anywhere connecting to cloud in the server logs:

```text
component=anywhere ... connected to cloud
```

You can also confirm from the cluster side:

```bash
miren cluster list
```

A registered cluster that has connected through Miren Anywhere will show up here.

## Troubleshooting

:::warning[Cluster won't connect]
Miren Anywhere requires outbound connectivity to miren.cloud (TLS over TCP on port 443) and to every host under `pop-global.miren.cloud` (HTTP/3 over UDP on port 8443). If your network blocks outbound traffic, check that the POP host (visible in server logs under `component=anywhere`) is reachable.
:::

:::warning[Hostnames return cloud errors]
If a request to `mycluster.run.garden` lands at the POP but fails to forward, the Miren Anywhere connection may be down. Restart the miren server and watch the logs for the `connected to cloud` marker.
:::

## Next Steps

- [Subdomains](/miren-cloud/subdomains) — Claim a hostname that uses Miren Anywhere
- [Miren Cloud Overview](/miren-cloud/overview) — Cluster registration and login
