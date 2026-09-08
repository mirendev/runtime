---
title: "Commands"
description: "Complete reference for all miren CLI commands"
keywords: [cli, commands, reference, miren]
---

# Commands

Complete reference for all `miren` CLI commands.

## addon

| Command | Description |
|---------|-------------|
| [`miren addon`](./command/addon.md) | Addon management commands |
| [`miren addon create`](./command/addon-create.md) | Attach an addon to an application |
| [`miren addon destroy`](./command/addon-destroy.md) | Remove an addon from an application |
| [`miren addon list`](./command/addon-list.md) | List addons attached to an application |
| [`miren addon list-available`](./command/addon-list-available.md) | List available addons |
| [`miren addon rotate`](./command/addon-rotate.md) | Rotate an addon's backing credential |
| [`miren addon variants`](./command/addon-variants.md) | Show variants for an addon |

## admin

| Command | Description |
|---------|-------------|
| [`miren admin`](./command/admin.md) | Call an admin method on an application |

## alias

| Command | Description |
|---------|-------------|
| [`miren alias`](./command/alias.md) | CLI alias management |
| [`miren alias list`](./command/alias-list.md) | List configured CLI aliases |

## app

| Command | Description |
|---------|-------------|
| [`miren app`](./command/app.md) | Get information about an application |
| [`miren app attach`](./command/app-attach.md) | Attach to a running task |
| [`miren app delete`](./command/app-delete.md) | Delete an application and all its resources |
| [`miren app history`](./command/app-history.md) | Show deployment history for an application |
| [`miren app list`](./command/app-list.md) | List all applications |
| [`miren app restart`](./command/app-restart.md) | Restart an application |
| [`miren app run`](./command/app-run.md) | Open interactive shell in a new sandbox |
| [`miren app runs`](./command/app-runs.md) | List recent task runs |
| [`miren app runs cancel`](./command/app-runs-cancel.md) | End a run early |
| [`miren app set-workload-role`](./command/app-set-workload-role.md) | Set the API role for an app's sandbox identity tokens |
| [`miren app status`](./command/app-status.md) | Show current status of an application |
| [`miren app versions`](./command/app-versions.md) | List app versions with status |

## apps

| Command | Description |
|---------|-------------|
| [`miren apps`](./command/apps.md) | List all applications (alias for 'app list') |

## auth

| Command | Description |
|---------|-------------|
| [`miren auth`](./command/auth.md) | Authentication commands |
| [`miren auth ci`](./command/auth-ci.md) | CI authentication binding management |
| [`miren auth ci add`](./command/auth-ci-add.md) | Add a CI authentication binding to an application |
| [`miren auth ci list`](./command/auth-ci-list.md) | List CI authentication bindings for an application |
| [`miren auth ci remove`](./command/auth-ci-remove.md) | Remove a CI authentication binding |
| [`miren auth generate`](./command/auth-generate.md) | Generate authentication config file |
| [`miren auth provider`](./command/auth-provider.md) | Identity provider management |
| [`miren auth provider add`](./command/auth-provider-add.md) | Add an identity provider for route protection |
| [`miren auth provider add github`](./command/auth-provider-add-github.md) | Add a GitHub identity provider |
| [`miren auth provider add oidc`](./command/auth-provider-add-oidc.md) | Add an OIDC identity provider |
| [`miren auth provider add password`](./command/auth-provider-add-password.md) | Add a shared-password identity provider |
| [`miren auth provider list`](./command/auth-provider-list.md) | List identity providers |
| [`miren auth provider remove`](./command/auth-provider-remove.md) | Remove an identity provider |
| [`miren auth provider show`](./command/auth-provider-show.md) | Show an identity provider |

## cluster

| Command | Description |
|---------|-------------|
| [`miren cluster`](./command/cluster.md) | List configured clusters |
| [`miren cluster add`](./command/cluster-add.md) | Add a new cluster configuration |
| [`miren cluster available`](./command/cluster-available.md) | List the clusters Miren Cloud has for your account |
| [`miren cluster current`](./command/cluster-current.md) | Show the pinned cluster for this app |
| [`miren cluster export-address`](./command/cluster-export-address.md) | Export cluster address with TLS fingerprint for MIREN_CLUSTER |
| [`miren cluster list`](./command/cluster-list.md) | List all configured clusters |
| [`miren cluster remove`](./command/cluster-remove.md) | Remove a cluster from the configuration |
| [`miren cluster switch`](./command/cluster-switch.md) | Switch to a different cluster |

## config

| Command | Description |
|---------|-------------|
| [`miren config`](./command/config.md) | Configuration file management |
| [`miren config info`](./command/config-info.md) | Show configuration file locations and format |
| [`miren config load`](./command/config-load.md) | Load config and merge it with your current config |

## deploy

| Command | Description |
|---------|-------------|
| [`miren deploy`](./command/deploy.md) | Deploy an application |
| [`miren deploy cancel`](./command/deploy-cancel.md) | Cancel an in-progress deployment |

## disk

| Command | Description |
|---------|-------------|
| [`miren disk`](./command/disk.md) | Disk backup and recovery |
| [`miren disk backup`](./command/disk-backup.md) | Backup a disk to a snapshot file |
| [`miren disk list-deleted`](./command/disk-list-deleted.md) | List deleted disks available for recovery |
| [`miren disk restore`](./command/disk-restore.md) | Restore a disk from a snapshot file |
| [`miren disk undelete`](./command/disk-undelete.md) | Restore a recently deleted disk |

## doctor

| Command | Description |
|---------|-------------|
| [`miren doctor`](./command/doctor.md) | Diagnose miren environment and connectivity |
| [`miren doctor auth`](./command/doctor-auth.md) | Check authentication and user information |
| [`miren doctor config`](./command/doctor-config.md) | Check configuration files |
| [`miren doctor server`](./command/doctor-server.md) | Check server health and connectivity |

## download

| Command | Description |
|---------|-------------|
| [`miren download`](./command/download.md) | Download management commands |
| [`miren download release`](./command/download-release.md) | Download and extract miren release |

## env

| Command | Description |
|---------|-------------|
| [`miren env`](./command/env.md) | Environment variable management commands |
| [`miren env delete`](./command/env-delete.md) | Delete environment variables |
| [`miren env get`](./command/env-get.md) | Get an environment variable value |
| [`miren env list`](./command/env-list.md) | List all environment variables |
| [`miren env set`](./command/env-set.md) | Set environment variables for an application |

## help

| Command | Description |
|---------|-------------|
| [`miren help`](./command/help.md) | Show help for one or more commands |

## init

| Command | Description |
|---------|-------------|
| [`miren init`](./command/init.md) | Initialize a new application |

## login

| Command | Description |
|---------|-------------|
| [`miren login`](./command/login.md) | Authenticate with miren.cloud |

## logout

| Command | Description |
|---------|-------------|
| [`miren logout`](./command/logout.md) | Remove local authentication credentials |

## logs

| Command | Description |
|---------|-------------|
| [`miren logs`](./command/logs.md) | View logs (defaults to app logs) |
| [`miren logs app`](./command/logs-app.md) | View application logs |
| [`miren logs build`](./command/logs-build.md) | View build logs |
| [`miren logs run`](./command/logs-run.md) | View logs for a task run |
| [`miren logs sandbox`](./command/logs-sandbox.md) | View sandbox logs |
| [`miren logs system`](./command/logs-system.md) | View system logs |

## rollback

| Command | Description |
|---------|-------------|
| [`miren rollback`](./command/rollback.md) | Roll back to a previous version |

## route

| Command | Description |
|---------|-------------|
| [`miren route`](./command/route.md) | List all HTTP routes |
| [`miren route down`](./command/route-down.md) | Put an HTTP route into maintenance |
| [`miren route list`](./command/route-list.md) | List all HTTP routes |
| [`miren route protect`](./command/route-protect.md) | Protect an HTTP route with an identity provider |
| [`miren route remove`](./command/route-remove.md) | Remove an HTTP route |
| [`miren route set`](./command/route-set.md) | Create or update an HTTP route |
| [`miren route set-default`](./command/route-set-default.md) | Set an app as the default route |
| [`miren route show`](./command/route-show.md) | Show details of an HTTP route |
| [`miren route timeout`](./command/route-timeout.md) | Override the ingress request timeout for an HTTP route |
| [`miren route unprotect`](./command/route-unprotect.md) | Remove identity-provider protection from an HTTP route |
| [`miren route unset-default`](./command/route-unset-default.md) | Remove the default route |
| [`miren route up`](./command/route-up.md) | Bring an HTTP route out of maintenance |
| [`miren route waf`](./command/route-waf.md) | Manage WAF protection on an HTTP route |

## runner

| Command | Description |
|---------|-------------|
| [`miren runner`](./command/runner.md) | Runner management commands |
| [`miren runner cordon`](./command/runner-cordon.md) | Mark a runner unschedulable without stopping its sandboxes |
| [`miren runner drain`](./command/runner-drain.md) | Cordon a runner and evict its sandboxes onto other nodes |
| [`miren runner install`](./command/runner-install.md) | Install systemd service for miren runner |
| [`miren runner join`](./command/runner-join.md) | Join this machine to a coordinator as a runner |
| [`miren runner list`](./command/runner-list.md) | List all registered runners |
| [`miren runner reissue`](./command/runner-reissue.md) | Rotate this runner's certificate in place (requires a still-valid cert), keeping its identity |
| [`miren runner remove`](./command/runner-remove.md) | Remove a registered runner and clean up resources |
| [`miren runner service-status`](./command/runner-service-status.md) | Show miren-runner systemd service status |
| [`miren runner start`](./command/runner-start.md) | Start this machine as a distributed runner |
| [`miren runner status`](./command/runner-status.md) | Show runner health and configuration |
| [`miren runner token`](./command/runner-token.md) | Manage join tokens |
| [`miren runner token create`](./command/runner-token-create.md) | Create a join token for a runner |
| [`miren runner token list`](./command/runner-token-list.md) | List all join tokens |
| [`miren runner token revoke`](./command/runner-token-revoke.md) | Revoke a join token |
| [`miren runner uncordon`](./command/runner-uncordon.md) | Make a cordoned runner eligible for scheduling again |
| [`miren runner uninstall`](./command/runner-uninstall.md) | Remove systemd service for miren runner |
| [`miren runner upgrade`](./command/runner-upgrade.md) | Upgrade miren runner to the latest or specified version |
| [`miren runner upgrade rollback`](./command/runner-upgrade-rollback.md) | Rollback runner to previous version |

## sandbox

| Command | Description |
|---------|-------------|
| [`miren sandbox`](./command/sandbox.md) | Sandbox management commands |
| [`miren sandbox delete`](./command/sandbox-delete.md) | Delete a dead sandbox |
| [`miren sandbox exec`](./command/sandbox-exec.md) | Open interactive shell in an existing sandbox |
| [`miren sandbox list`](./command/sandbox-list.md) | List sandboxes (excludes dead by default) |
| [`miren sandbox stop`](./command/sandbox-stop.md) | Stop a sandbox |

## sandbox-pool

| Command | Description |
|---------|-------------|
| [`miren sandbox-pool`](./command/sandbox-pool.md) | Sandbox pool management commands |
| [`miren sandbox-pool list`](./command/sandbox-pool-list.md) | List all sandbox pools |
| [`miren sandbox-pool set-desired`](./command/sandbox-pool-set-desired.md) | Set desired instance count for a sandbox pool |

## secret

| Command | Description |
|---------|-------------|
| [`miren secret`](./command/secret.md) | Secret store management commands |
| [`miren secret destroy`](./command/secret-destroy.md) | Permanently delete a version's value |
| [`miren secret disable`](./command/secret-disable.md) | Stop a version from resolving |
| [`miren secret enable`](./command/secret-enable.md) | Let a disabled version resolve again |
| [`miren secret keyring`](./command/secret-keyring.md) | Show the cluster keyring and any rotation in flight |
| [`miren secret list`](./command/secret-list.md) | List stored secrets |
| [`miren secret rotate-key`](./command/secret-rotate-key.md) | Rotate the cluster key that encrypts stored secrets |
| [`miren secret set`](./command/secret-set.md) | Store a secret value |
| [`miren secret versions`](./command/secret-versions.md) | Show a secret's versions |

## server

| Command | Description |
|---------|-------------|
| [`miren server`](./command/server.md) | Start the miren server |
| [`miren server config`](./command/server-config.md) | Server configuration management commands |
| [`miren server config generate`](./command/server-config-generate.md) | Generate a server configuration file from current settings |
| [`miren server config validate`](./command/server-config-validate.md) | Validate a server configuration file |
| [`miren server container`](./command/server-container.md) | Run the miren server in a container (Docker or Podman) |
| [`miren server container install`](./command/server-container-install.md) | Install miren server in a container |
| [`miren server container status`](./command/server-container-status.md) | Show status of miren server container |
| [`miren server container uninstall`](./command/server-container-uninstall.md) | Uninstall miren server container |
| [`miren server identity-anchor`](./command/server-identity-anchor.md) | Move where this cluster's workload identity is anchored |
| [`miren server install`](./command/server-install.md) | Install systemd service for miren server |
| [`miren server register`](./command/server-register.md) | Register this cluster with miren.cloud |
| [`miren server register status`](./command/server-register-status.md) | Show cluster registration status |
| [`miren server status`](./command/server-status.md) | Show miren service status |
| [`miren server uninstall`](./command/server-uninstall.md) | Remove systemd service for miren server |
| [`miren server unregister`](./command/server-unregister.md) | Detach this cluster from miren.cloud |
| [`miren server upgrade`](./command/server-upgrade.md) | Upgrade miren server |
| [`miren server upgrade rollback`](./command/server-upgrade-rollback.md) | Rollback server to previous version |

## upgrade

| Command | Description |
|---------|-------------|
| [`miren upgrade`](./command/upgrade.md) | Upgrade miren CLI to latest version |

## version

| Command | Description |
|---------|-------------|
| [`miren version`](./command/version.md) | Print the version |

## whoami

| Command | Description |
|---------|-------------|
| [`miren whoami`](./command/whoami.md) | Display information about the current authenticated user |

---

## Advanced / Debug Commands

:::warning
These commands are intended for advanced debugging and troubleshooting. They may change without notice.
:::

| Command | Description |
|---------|-------------|
| [`miren debug`](./command/debug.md) | Debug and troubleshooting commands |
| [`miren debug advertise`](./command/debug-advertise.md) | Show which addresses the server would advertise and why |
| [`miren debug bundle`](./command/debug-bundle.md) | Create a support bundle with system debug information |
| [`miren debug cloud-sync`](./command/debug-cloud-sync.md) | Show runtime entity sync diagnostics |
| [`miren debug colors`](./command/debug-colors.md) | Print some colors |
| [`miren debug connection`](./command/debug-connection.md) | Test connectivity and authentication with a server |
| [`miren debug ctr`](./command/debug-ctr.md) | Run ctr with miren defaults |
| [`miren debug ctr nuke`](./command/debug-ctr-nuke.md) | Nuke a containerd namespace |
| [`miren debug disk`](./command/debug-disk.md) | Disk entity debug commands |
| [`miren debug disk backup`](./command/debug-disk-backup.md) | Back up a disk by reading its image directly (break-glass) |
| [`miren debug disk create`](./command/debug-disk-create.md) | Create a disk entity for testing |
| [`miren debug disk delete`](./command/debug-disk-delete.md) | Delete a disk entity |
| [`miren debug disk lease`](./command/debug-disk-lease.md) | Create a disk lease for testing |
| [`miren debug disk lease-delete`](./command/debug-disk-lease-delete.md) | Delete a disk lease entity |
| [`miren debug disk lease-list`](./command/debug-disk-lease-list.md) | List all disk lease entities |
| [`miren debug disk lease-release`](./command/debug-disk-lease-release.md) | Release a disk lease |
| [`miren debug disk lease-status`](./command/debug-disk-lease-status.md) | Show detailed status of a disk lease |
| [`miren debug disk list`](./command/debug-disk-list.md) | List all disk entities |
| [`miren debug disk list-deleted`](./command/debug-disk-list-deleted.md) | Read the soft-delete holding area directly (break-glass) |
| [`miren debug disk mounts`](./command/debug-disk-mounts.md) | List all mounted disks from /proc/mounts |
| [`miren debug disk restore`](./command/debug-disk-restore.md) | Restore a disk by writing its image directly (break-glass) |
| [`miren debug disk status`](./command/debug-disk-status.md) | Show status of a disk entity |
| [`miren debug disk undelete`](./command/debug-disk-undelete.md) | Recover a deleted disk by moving its data directly (break-glass) |
| [`miren debug entity`](./command/debug-entity.md) | Entity store debug commands |
| [`miren debug entity create`](./command/debug-entity-create.md) | Create a new entity |
| [`miren debug entity delete`](./command/debug-entity-delete.md) | Delete an entity |
| [`miren debug entity ensure`](./command/debug-entity-ensure.md) | Ensure an entity exists |
| [`miren debug entity get`](./command/debug-entity-get.md) | Get an entity |
| [`miren debug entity list`](./command/debug-entity-list.md) | List entities |
| [`miren debug entity patch`](./command/debug-entity-patch.md) | Patch an existing entity |
| [`miren debug entity put`](./command/debug-entity-put.md) | Put an entity |
| [`miren debug entity replace`](./command/debug-entity-replace.md) | Replace an existing entity |
| [`miren debug etcdctl`](./command/debug-etcdctl.md) | Run etcdctl against Miren's embedded etcd |
| [`miren debug netdb`](./command/debug-netdb.md) | Network database debug commands |
| [`miren debug netdb gc`](./command/debug-netdb-gc.md) | Find and release orphaned IP leases |
| [`miren debug netdb list`](./command/debug-netdb-list.md) | List all IP leases from netdb |
| [`miren debug netdb release`](./command/debug-netdb-release.md) | Manually release IP leases |
| [`miren debug netdb status`](./command/debug-netdb-status.md) | Show IP allocation status by subnet |
| [`miren debug rbac`](./command/debug-rbac.md) | Fetch and display RBAC rules from miren.cloud |
| [`miren debug rbac test`](./command/debug-rbac-test.md) | Test RBAC evaluation with fetched rules |
| [`miren debug reindex`](./command/debug-reindex.md) | Rebuild all entity indexes from scratch |
| [`miren debug saga`](./command/debug-saga.md) | Saga execution debug commands |
| [`miren debug saga list`](./command/debug-saga-list.md) | List saga executions |
| [`miren debug saga show`](./command/debug-saga-show.md) | Show a saga execution in detail |
| [`miren debug test`](./command/debug-test.md) | Debug test commands |
| [`miren debug test load`](./command/debug-test-load.md) | Loadtest a URL |

