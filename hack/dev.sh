#!/usr/bin/env bash
set -e

# Source common setup functions
source "$(dirname "$0")/common-setup.sh"

# Setup environment
setup_cgroups
setup_environment

# In standalone mode, miren manages its own containerd
export CONTAINERD_ADDRESS="/var/lib/miren/containerd/containerd.sock"

# Setup kernel mounts
setup_kernel_mounts

cd /src

# Build miren
make bin/miren

# Create symlink
ln -sf "$PWD"/bin/miren /bin/m

# Setup miren config
mkdir -p ~/.config/miren
m auth generate -c ~/.config/miren/clientconfig.yaml

# Setup release directory for standalone mode
echo "Setting up release directory for standalone mode..."
mkdir -p /var/lib/miren/release
cp bin/miren /var/lib/miren/release/
cp /usr/local/bin/runc /var/lib/miren/release/
cp /usr/local/bin/containerd-shim-runsc-v1 /var/lib/miren/release/
cp /usr/local/bin/containerd-shim-runc-v2 /var/lib/miren/release/
cp /usr/local/bin/containerd /var/lib/miren/release/
cp /usr/local/bin/nerdctl /var/lib/miren/release/
cp /usr/local/bin/ctr /var/lib/miren/release/

# Setup environment variables
setup_bash_environment

# Clear stale containerd state to avoid race condition on fresh boot
# Keep root directory (images/snapshots) for performance, but clear state (runtime metadata)
# This prevents the sandbox reconciler from cleaning up newly created containers
# that it mistakes for stale containers from previous dev sessions
if [ -d /var/lib/miren/containerd/state ]; then
    echo "Cleaning stale containerd state..."
    rm -rf /var/lib/miren/containerd/state/*
fi

echo ""
echo "✓ Development environment ready!"
echo ""
echo "  Server command: m server -vv --mode standalone"
echo ""
echo "Useful commands:"
echo "  make dev-server-start   # Start miren server"
echo "  make dev-server-status  # Check server status"
echo "  make dev-server-logs    # Watch server logs"
echo "  make dev-server-restart # Restart after code changes"
echo "  m app list              # Use miren CLI"
echo ""
