# Miren Systemd Test Container

This directory contains a Docker-based test environment that mirrors the production Miren setup with systemd.

## Quick Start

```bash
# From repository root
./hack/systemd/run-systemd-test.sh

# Or from within the hack/systemd directory
bash run-systemd-test.sh
```

This will:
1. Build an Ubuntu 24.04 container with systemd
2. Start the container and automatically:
   - Install Miren to `/var/lib/miren/release/` (like production)
   - Run `miren server install` to create the unit and its resource-limit drop-in
   - Create symlink at `/usr/local/bin/miren`
   - Verify the loaded unit carries the directives it should
3. Provide a fully configured environment matching production

## Testing local changes

If `bin/miren` exists in the repository, the container runs that binary instead
of a published build, so a change that hasn't been pushed anywhere can still be
tested end to end:

```bash
make bin/miren
./hack/systemd/run-systemd-test.sh
```

The release bundle for `$RELEASE` is still downloaded, because the container
needs containerd, runc and the shims from it. Only `miren` itself comes from
your build. The entrypoint copies it in twice on purpose: once before
`server install`, and again afterwards, because `server install` downloads the
bundle and promotes the whole directory into place, which replaces the first
copy. Without the second copy the harness would install with your binary and
then test the downloaded one.

## Files

- `Dockerfile.systemd` - Ubuntu 24.04 with systemd and necessary tools
- `entrypoint.sh` - Script that mirrors production installation process
- `run-systemd-test.sh` - Script to build and run the test container
- `test-oom-restart.sh` - End-to-end check of the memory limit and its reporting

## Production Mirroring

The container setup mirrors the production installation exactly:
- Same directory structure (`/var/lib/miren/release/`)
- The unit comes from `miren server install`, not from a copy in this directory.
  It used to be a heredoc here, which quietly went stale — the harness spent
  months testing a unit that was missing `ExecReload`.
- Miren brings its own containerd (no Docker needed)

## Testing resource limits

The container runs `--privileged --cgroupns=host` with `/sys/fs/cgroup` mounted
read-write, so cgroup v2 limits genuinely apply inside it.

```bash
# What systemd resolved from the unit plus its drop-in
docker exec miren-systemd-test systemctl show -p MemoryMax -p MemoryHigh -p MemorySwapMax miren

# Squeeze the service until the kernel kills it, then check the kill was
# recorded and reported on the next start
docker exec -it miren-systemd-test /hack/test-oom-restart.sh
```

## Upgrade End-to-End Test

`./hack/systemd/upgrade-e2e.sh` runs the whole restart/upgrade/rollback path
against this container using builds of the current checkout and a local asset
server, so nothing has to be published first. It takes several minutes and
needs the iso dev environment for the builds. Set `WORK=/some/dir` to keep the
fixtures between runs.

## Testing Upgrades

The container starts with miren pre-installed:

```bash
# Enter the container
docker exec -it miren-systemd-test bash

# Check installed version
miren version

# Start the service
systemctl start miren
systemctl status miren

# Test upgrade commands
miren upgrade --check                     # Check for updates
sudo miren server upgrade --version main  # Upgrade to main branch
sudo miren server upgrade rollback        # Rollback to previous
```

## Customizing Branch

The container installs from the `main` branch by default. To test with a different branch:

```bash
# Test with simplified-upgrade branch
RELEASE=simplified-upgrade ./hack/systemd/run-systemd-test.sh

# Test with any other branch
RELEASE=feature-xyz ./hack/systemd/run-systemd-test.sh
```

## Viewing Logs

```bash
# Container setup logs
docker logs miren-systemd-test

# Miren service logs
docker exec -it miren-systemd-test journalctl -u miren -f
```

## Cleanup

```bash
# Stop and remove container
docker rm -f miren-systemd-test

# Remove persistent data volume (optional)
docker volume rm miren-data
```

## Notes

- The container uses a Docker volume `miren-data` mounted at `/var/lib/miren` to provide a proper filesystem for containerd's overlay operations
- This volume persists between container restarts, allowing you to test upgrades across container recreations
- To completely reset, remove both the container and the volume

## Requirements

- Linux host (systemd in Docker requires Linux)
- Docker with cgroupv2 support
- Sufficient privileges to run privileged containers