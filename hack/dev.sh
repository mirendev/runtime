#!/usr/bin/env bash
set -e

source "$(dirname "$0")/common-setup.sh"

setup_cgroups
setup_environment
setup_host_user
setup_kernel_mounts

cd /src

echo "Building miren as host user (UID ${ISO_UID})..."
su -s /bin/bash "$HOST_USER" -c "make bin/miren"

ln -sf "$PWD"/bin/miren /bin/m

mkdir -p /var/lib/miren/server
chown -R "$HOST_UID:$HOST_GID" /var/lib/miren
su -s /bin/bash "$HOST_USER" -c "mkdir -p ~/.config/miren && m auth generate -c ~/.config/miren/clientconfig.yaml"

echo "Setting up release directory for standalone mode..."
mkdir -p /var/lib/miren/release
cp bin/miren /var/lib/miren/release/
cp /usr/local/bin/runc /var/lib/miren/release/
cp /usr/local/bin/containerd-shim-runsc-v1 /var/lib/miren/release/
cp /usr/local/bin/containerd-shim-runc-v2 /var/lib/miren/release/
cp /usr/local/bin/containerd /var/lib/miren/release/
cp /usr/local/bin/nerdctl /var/lib/miren/release/
cp /usr/local/bin/ctr /var/lib/miren/release/

echo ""
echo "✓ Development environment ready!"
echo ""
echo "Useful commands:"
echo "  make dev-server-start   # Start miren server"
echo "  make dev-server-status  # Check server status"
echo "  make dev-server-logs    # Watch server logs"
echo "  make dev-server-restart # Restart after code changes"
echo "  m sandbox list          # Use miren CLI"
echo ""
