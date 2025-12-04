---
sidebar_position: 3
---

# Working with Miren Cloud

Miren Cloud is a central control plane that connects and manages your Miren clusters. While Miren runs fully standalone on your own infrastructure, connecting to Miren Cloud gives you:
- Team management and access control
- Automatic data backup and sync
- Multi-environment workflows

## Miren Server Installation (with Cloud)

When you run `miren server install`, it will automatically register a new cluster to Miren Cloud and redirect you to create your miren.cloud organization and account:

```bash
sudo miren server install
```

By default, you will have full access to your new cluster. Permissions can be tweaked using RBAC rules if needed.

To skip cloud registration and run standalone:

```bash
sudo miren server install --without-cloud
```

## Login

Authenticate with miren.cloud:

```bash
miren login
```

This will open a browser window to complete authentication.

## Check Your Identity

See who you're logged in as:

```bash
miren whoami
```

## Register Your Cluster

Connect your local cluster to miren.cloud:

```bash
miren register -n my-cluster
```

This registers your cluster and enables cloud features.

## View Your Clusters

List all clusters associated with your account:

```bash
miren cluster list
```

## Switch Clusters

If you have multiple clusters, switch between them:

```bash
miren cluster switch my-other-cluster
```
