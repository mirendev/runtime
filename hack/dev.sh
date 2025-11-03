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

# Wait for minio to be ready (etcd and clickhouse are started by miren in standalone mode)
wait_for_service "minio" "nc -z minio 9000"

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
