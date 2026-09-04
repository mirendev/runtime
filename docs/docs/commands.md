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
| [`miren addon`](/command/addon) | Addon management commands |
| [`miren addon create`](/command/addon-create) | Attach an addon to an application |
| [`miren addon destroy`](/command/addon-destroy) | Remove an addon from an application |
| [`miren addon list`](/command/addon-list) | List addons attached to an application |
| [`miren addon list-available`](/command/addon-list-available) | List available addons |
| [`miren addon rotate`](/command/addon-rotate) | Rotate an addon's backing credential |
| [`miren addon variants`](/command/addon-variants) | Show variants for an addon |

## admin

| Command | Description |
|---------|-------------|
| [`miren admin`](/command/admin) | Call an admin method on an application |

## alias

| Command | Description |
|---------|-------------|
| [`miren alias`](/command/alias) | CLI alias management |
| [`miren alias list`](/command/alias-list) | List configured CLI aliases |

## app

| Command | Description |
|---------|-------------|
| [`miren app`](/command/app) | Get information about an application |
| [`miren app attach`](/command/app-attach) | Attach to a running task |
| [`miren app delete`](/command/app-delete) | Delete an application and all its resources |
| [`miren app history`](/command/app-history) | Show deployment history for an application |
| [`miren app list`](/command/app-list) | List all applications |
| [`miren app restart`](/command/app-restart) | Restart an application |
| [`miren app run`](/command/app-run) | Open interactive shell in a new sandbox |
| [`miren app runs`](/command/app-runs) | List recent task runs |
| [`miren app runs cancel`](/command/app-runs-cancel) | End a run early |
| [`miren app set-workload-role`](/command/app-set-workload-role) | Set the API role for an app's sandbox identity tokens |
| [`miren app status`](/command/app-status) | Show current status of an application |
| [`miren app versions`](/command/app-versions) | List app versions with status |

## apps

| Command | Description |
|---------|-------------|
| [`miren apps`](/command/apps) | List all applications (alias for 'app list') |

## auth

| Command | Description |
|---------|-------------|
| [`miren auth`](/command/auth) | Authentication commands |
| [`miren auth ci`](/command/auth-ci) | CI authentication binding management |
| [`miren auth ci add`](/command/auth-ci-add) | Add a CI authentication binding to an application |
| [`miren auth ci list`](/command/auth-ci-list) | List CI authentication bindings for an application |
| [`miren auth ci remove`](/command/auth-ci-remove) | Remove a CI authentication binding |
| [`miren auth generate`](/command/auth-generate) | Generate authentication config file |
| [`miren auth provider`](/command/auth-provider) | Identity provider management |
| [`miren auth provider add`](/command/auth-provider-add) | Add an identity provider for route protection |
| [`miren auth provider add github`](/command/auth-provider-add-github) | Add a GitHub identity provider |
| [`miren auth provider add oidc`](/command/auth-provider-add-oidc) | Add an OIDC identity provider |
| [`miren auth provider add password`](/command/auth-provider-add-password) | Add a shared-password identity provider |
| [`miren auth provider list`](/command/auth-provider-list) | List identity providers |
| [`miren auth provider remove`](/command/auth-provider-remove) | Remove an identity provider |
| [`miren auth provider show`](/command/auth-provider-show) | Show an identity provider |

## cluster

| Command | Description |
|---------|-------------|
| [`miren cluster`](/command/cluster) | List configured clusters |
| [`miren cluster add`](/command/cluster-add) | Add a new cluster configuration |
| [`miren cluster available`](/command/cluster-available) | List the clusters Miren Cloud has for your account |
| [`miren cluster current`](/command/cluster-current) | Show the pinned cluster for this app |
| [`miren cluster export-address`](/command/cluster-export-address) | Export cluster address with TLS fingerprint for MIREN_CLUSTER |
| [`miren cluster list`](/command/cluster-list) | List all configured clusters |
| [`miren cluster remove`](/command/cluster-remove) | Remove a cluster from the configuration |
| [`miren cluster switch`](/command/cluster-switch) | Switch to a different cluster |

## config

| Command | Description |
|---------|-------------|
| [`miren config`](/command/config) | Configuration file management |
| [`miren config info`](/command/config-info) | Show configuration file locations and format |
| [`miren config load`](/command/config-load) | Load config and merge it with your current config |

## deploy

| Command | Description |
|---------|-------------|
| [`miren deploy`](/command/deploy) | Deploy an application |
| [`miren deploy cancel`](/command/deploy-cancel) | Cancel an in-progress deployment |

## disk

| Command | Description |
|---------|-------------|
| [`miren disk`](/command/disk) | Disk backup and recovery |
| [`miren disk backup`](/command/disk-backup) | Backup a disk to a snapshot file |
| [`miren disk list-deleted`](/command/disk-list-deleted) | List deleted disks available for recovery |
| [`miren disk restore`](/command/disk-restore) | Restore a disk from a snapshot file |
| [`miren disk undelete`](/command/disk-undelete) | Restore a recently deleted disk |

## doctor

| Command | Description |
|---------|-------------|
| [`miren doctor`](/command/doctor) | Diagnose miren environment and connectivity |
| [`miren doctor auth`](/command/doctor-auth) | Check authentication and user information |
| [`miren doctor config`](/command/doctor-config) | Check configuration files |
| [`miren doctor server`](/command/doctor-server) | Check server health and connectivity |

## download

| Command | Description |
|---------|-------------|
| [`miren download`](/command/download) | Download management commands |
| [`miren download release`](/command/download-release) | Download and extract miren release |

## env

| Command | Description |
|---------|-------------|
| [`miren env`](/command/env) | Environment variable management commands |
| [`miren env delete`](/command/env-delete) | Delete environment variables |
| [`miren env get`](/command/env-get) | Get an environment variable value |
| [`miren env list`](/command/env-list) | List all environment variables |
| [`miren env set`](/command/env-set) | Set environment variables for an application |

## help

| Command | Description |
|---------|-------------|
| [`miren help`](/command/help) | Show help for one or more commands |

## init

| Command | Description |
|---------|-------------|
| [`miren init`](/command/init) | Initialize a new application |

## login

| Command | Description |
|---------|-------------|
| [`miren login`](/command/login) | Authenticate with miren.cloud |

## logout

| Command | Description |
|---------|-------------|
| [`miren logout`](/command/logout) | Remove local authentication credentials |

## logs

| Command | Description |
|---------|-------------|
| [`miren logs`](/command/logs) | View logs (defaults to app logs) |
| [`miren logs app`](/command/logs-app) | View application logs |
| [`miren logs build`](/command/logs-build) | View build logs |
| [`miren logs run`](/command/logs-run) | View logs for a task run |
| [`miren logs sandbox`](/command/logs-sandbox) | View sandbox logs |
| [`miren logs system`](/command/logs-system) | View system logs |

## rollback

| Command | Description |
|---------|-------------|
| [`miren rollback`](/command/rollback) | Roll back to a previous version |

## route

| Command | Description |
|---------|-------------|
| [`miren route`](/command/route) | List all HTTP routes |
| [`miren route down`](/command/route-down) | Put an HTTP route into maintenance |
| [`miren route list`](/command/route-list) | List all HTTP routes |
| [`miren route protect`](/command/route-protect) | Protect an HTTP route with an identity provider |
| [`miren route remove`](/command/route-remove) | Remove an HTTP route |
| [`miren route set`](/command/route-set) | Create or update an HTTP route |
| [`miren route set-default`](/command/route-set-default) | Set an app as the default route |
| [`miren route show`](/command/route-show) | Show details of an HTTP route |
| [`miren route timeout`](/command/route-timeout) | Override the ingress request timeout for an HTTP route |
| [`miren route unprotect`](/command/route-unprotect) | Remove identity-provider protection from an HTTP route |
| [`miren route unset-default`](/command/route-unset-default) | Remove the default route |
| [`miren route up`](/command/route-up) | Bring an HTTP route out of maintenance |
| [`miren route waf`](/command/route-waf) | Manage WAF protection on an HTTP route |

## runner

| Command | Description |
|---------|-------------|
| [`miren runner`](/command/runner) | Runner management commands |
| [`miren runner cordon`](/command/runner-cordon) | Mark a runner unschedulable without stopping its sandboxes |
| [`miren runner drain`](/command/runner-drain) | Cordon a runner and evict its sandboxes onto other nodes |
| [`miren runner install`](/command/runner-install) | Install systemd service for miren runner |
| [`miren runner join`](/command/runner-join) | Join this machine to a coordinator as a runner |
| [`miren runner list`](/command/runner-list) | List all registered runners |
| [`miren runner reissue`](/command/runner-reissue) | Rotate this runner's certificate in place (requires a still-valid cert), keeping its identity |
| [`miren runner remove`](/command/runner-remove) | Remove a registered runner and clean up resources |
| [`miren runner service-status`](/command/runner-service-status) | Show miren-runner systemd service status |
| [`miren runner start`](/command/runner-start) | Start this machine as a distributed runner |
| [`miren runner status`](/command/runner-status) | Show runner health and configuration |
| [`miren runner token`](/command/runner-token) | Manage join tokens |
| [`miren runner token create`](/command/runner-token-create) | Create a join token for a runner |
| [`miren runner token list`](/command/runner-token-list) | List all join tokens |
| [`miren runner token revoke`](/command/runner-token-revoke) | Revoke a join token |
| [`miren runner uncordon`](/command/runner-uncordon) | Make a cordoned runner eligible for scheduling again |
| [`miren runner uninstall`](/command/runner-uninstall) | Remove systemd service for miren runner |
| [`miren runner upgrade`](/command/runner-upgrade) | Upgrade miren runner to the latest or specified version |
| [`miren runner upgrade rollback`](/command/runner-upgrade-rollback) | Rollback runner to previous version |

## sandbox

| Command | Description |
|---------|-------------|
| [`miren sandbox`](/command/sandbox) | Sandbox management commands |
| [`miren sandbox delete`](/command/sandbox-delete) | Delete a dead sandbox |
| [`miren sandbox exec`](/command/sandbox-exec) | Open interactive shell in an existing sandbox |
| [`miren sandbox list`](/command/sandbox-list) | List sandboxes (excludes dead by default) |
| [`miren sandbox stop`](/command/sandbox-stop) | Stop a sandbox |

## sandbox-pool

| Command | Description |
|---------|-------------|
| [`miren sandbox-pool`](/command/sandbox-pool) | Sandbox pool management commands |
| [`miren sandbox-pool list`](/command/sandbox-pool-list) | List all sandbox pools |
| [`miren sandbox-pool set-desired`](/command/sandbox-pool-set-desired) | Set desired instance count for a sandbox pool |

## secret

| Command | Description |
|---------|-------------|
| [`miren secret`](/command/secret) | Secret store management commands |
| [`miren secret destroy`](/command/secret-destroy) | Permanently delete a version's value |
| [`miren secret disable`](/command/secret-disable) | Stop a version from resolving |
| [`miren secret enable`](/command/secret-enable) | Let a disabled version resolve again |
| [`miren secret keyring`](/command/secret-keyring) | Show the cluster keyring and any rotation in flight |
| [`miren secret list`](/command/secret-list) | List stored secrets |
| [`miren secret rotate-key`](/command/secret-rotate-key) | Rotate the cluster key that encrypts stored secrets |
| [`miren secret set`](/command/secret-set) | Store a secret value |
| [`miren secret versions`](/command/secret-versions) | Show a secret's versions |

## server

| Command | Description |
|---------|-------------|
| [`miren server`](/command/server) | Start the miren server |
| [`miren server config`](/command/server-config) | Server configuration management commands |
| [`miren server config generate`](/command/server-config-generate) | Generate a server configuration file from current settings |
| [`miren server config validate`](/command/server-config-validate) | Validate a server configuration file |
| [`miren server container`](/command/server-container) | Run the miren server in a container (Docker or Podman) |
| [`miren server container install`](/command/server-container-install) | Install miren server in a container |
| [`miren server container status`](/command/server-container-status) | Show status of miren server container |
| [`miren server container uninstall`](/command/server-container-uninstall) | Uninstall miren server container |
| [`miren server identity-anchor`](/command/server-identity-anchor) | Move where this cluster's workload identity is anchored |
| [`miren server install`](/command/server-install) | Install systemd service for miren server |
| [`miren server register`](/command/server-register) | Register this cluster with miren.cloud |
| [`miren server register status`](/command/server-register-status) | Show cluster registration status |
| [`miren server status`](/command/server-status) | Show miren service status |
| [`miren server uninstall`](/command/server-uninstall) | Remove systemd service for miren server |
| [`miren server unregister`](/command/server-unregister) | Detach this cluster from miren.cloud |
| [`miren server upgrade`](/command/server-upgrade) | Upgrade miren server |
| [`miren server upgrade rollback`](/command/server-upgrade-rollback) | Rollback server to previous version |

## upgrade

| Command | Description |
|---------|-------------|
| [`miren upgrade`](/command/upgrade) | Upgrade miren CLI to latest version |

## version

| Command | Description |
|---------|-------------|
| [`miren version`](/command/version) | Print the version |

## whoami

| Command | Description |
|---------|-------------|
| [`miren whoami`](/command/whoami) | Display information about the current authenticated user |

---

## Advanced / Debug Commands

:::warning
These commands are intended for advanced debugging and troubleshooting. They may change without notice.
:::

| Command | Description |
|---------|-------------|
| [`miren debug`](/command/debug) | Debug and troubleshooting commands |
| [`miren debug advertise`](/command/debug-advertise) | Show which addresses the server would advertise and why |
| [`miren debug bundle`](/command/debug-bundle) | Create a support bundle with system debug information |
| [`miren debug colors`](/command/debug-colors) | Print some colors |
| [`miren debug connection`](/command/debug-connection) | Test connectivity and authentication with a server |
| [`miren debug ctr`](/command/debug-ctr) | Run ctr with miren defaults |
| [`miren debug ctr nuke`](/command/debug-ctr-nuke) | Nuke a containerd namespace |
| [`miren debug disk`](/command/debug-disk) | Disk entity debug commands |
| [`miren debug disk backup`](/command/debug-disk-backup) | Back up a disk by reading its image directly (break-glass) |
| [`miren debug disk create`](/command/debug-disk-create) | Create a disk entity for testing |
| [`miren debug disk delete`](/command/debug-disk-delete) | Delete a disk entity |
| [`miren debug disk lease`](/command/debug-disk-lease) | Create a disk lease for testing |
| [`miren debug disk lease-delete`](/command/debug-disk-lease-delete) | Delete a disk lease entity |
| [`miren debug disk lease-list`](/command/debug-disk-lease-list) | List all disk lease entities |
| [`miren debug disk lease-release`](/command/debug-disk-lease-release) | Release a disk lease |
| [`miren debug disk lease-status`](/command/debug-disk-lease-status) | Show detailed status of a disk lease |
| [`miren debug disk list`](/command/debug-disk-list) | List all disk entities |
| [`miren debug disk mounts`](/command/debug-disk-mounts) | List all mounted disks from /proc/mounts |
| [`miren debug disk restore`](/command/debug-disk-restore) | Restore a disk by writing its image directly (break-glass) |
| [`miren debug disk status`](/command/debug-disk-status) | Show status of a disk entity |
| [`miren debug entity`](/command/debug-entity) | Entity store debug commands |
| [`miren debug entity create`](/command/debug-entity-create) | Create a new entity |
| [`miren debug entity delete`](/command/debug-entity-delete) | Delete an entity |
| [`miren debug entity ensure`](/command/debug-entity-ensure) | Ensure an entity exists |
| [`miren debug entity get`](/command/debug-entity-get) | Get an entity |
| [`miren debug entity list`](/command/debug-entity-list) | List entities |
| [`miren debug entity patch`](/command/debug-entity-patch) | Patch an existing entity |
| [`miren debug entity put`](/command/debug-entity-put) | Put an entity |
| [`miren debug entity replace`](/command/debug-entity-replace) | Replace an existing entity |
| [`miren debug etcdctl`](/command/debug-etcdctl) | Run etcdctl against Miren's embedded etcd |
| [`miren debug netdb`](/command/debug-netdb) | Network database debug commands |
| [`miren debug netdb gc`](/command/debug-netdb-gc) | Find and release orphaned IP leases |
| [`miren debug netdb list`](/command/debug-netdb-list) | List all IP leases from netdb |
| [`miren debug netdb release`](/command/debug-netdb-release) | Manually release IP leases |
| [`miren debug netdb status`](/command/debug-netdb-status) | Show IP allocation status by subnet |
| [`miren debug rbac`](/command/debug-rbac) | Fetch and display RBAC rules from miren.cloud |
| [`miren debug rbac test`](/command/debug-rbac-test) | Test RBAC evaluation with fetched rules |
| [`miren debug reindex`](/command/debug-reindex) | Rebuild all entity indexes from scratch |
| [`miren debug saga`](/command/debug-saga) | Saga execution debug commands |
| [`miren debug saga list`](/command/debug-saga-list) | List saga executions |
| [`miren debug saga show`](/command/debug-saga-show) | Show a saga execution in detail |
| [`miren debug test`](/command/debug-test) | Debug test commands |
| [`miren debug test load`](/command/debug-test-load) | Loadtest a URL |

