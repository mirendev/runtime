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
   - Create systemd service at `/etc/systemd/system/miren.service`
   - Create symlink at `/usr/local/bin/miren`
3. Provide a fully configured environment matching production

## Files

- `Dockerfile.systemd` - Ubuntu 24.04 with systemd and necessary tools
- `entrypoint.sh` - Script that mirrors production installation process
- `run-systemd-test.sh` - Script to build and run the test container

## Production Mirroring

The container setup mirrors the production installation exactly:
- Same directory structure (`/var/lib/miren/release/`)
- Same systemd service configuration with `KillMode=process`
- Miren brings its own containerd (no Docker needed)

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