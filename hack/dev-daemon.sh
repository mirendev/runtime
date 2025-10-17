#!/usr/bin/env bash
set -e

# Source common setup functions
source /usr/local/bin/common-setup.sh

echo "=== Starting Miren Dev Environment ==="

# Setup environment
setup_cgroups
setup_environment

export CONTAINERD_ADDRESS="/var/lib/miren/containerd/containerd.sock"

# Generate configs with metrics enabled
generate_containerd_config "127.0.0.1:1338"
setup_runsc_config

# Start services with specific log destinations
start_containerd "$CONTAINERD_ADDRESS" "/tmp/containerd.log"
start_buildkitd "/tmp/buildkit.log"

# Setup kernel mounts
setup_kernel_mounts

# Wait for containerd to start
sleep 1

cd /src

# Wait for external services to be ready
wait_for_service "etcd" "nc -z etcd 2379"
wait_for_service "clickhouse" "nc -z clickhouse 9000"
wait_for_service "minio" "nc -z minio 9000"

# Build miren if go.mod exists (might not on first run)
if [ -f "go.mod" ]; then
    echo "Building miren binary..."
    make bin/miren || echo "Warning: Failed to build miren, will retry on first use"

    # Create symlink if binary exists
    if [ -f "bin/miren" ]; then
        ln -sf /src/bin/miren /bin/m

        # Setup miren config
        mkdir -p ~/.config/miren
        m auth generate -c ~/.config/miren/clientconfig.yaml 2>/dev/null || echo "Warning: Failed to generate auth config"

        echo "Cleaning miren namespace..."
        m debug ctr nuke -n miren --containerd-socket "$CONTAINERD_ADDRESS" 2>/dev/null || echo "No existing miren containers to clean"
    fi
fi

# Setup environment for bash sessions
setup_bash_environment

echo ""
echo "=== Miren Dev Environment Ready ==="
echo "Services:"
echo "  - etcd:       etcd:2379 (also localhost:2379)"
echo "  - clickhouse: clickhouse:9000 (also localhost:9000)"
echo "  - minio:      minio:9000 (also localhost:9001)"
echo ""
echo "Source code: /src"
echo "Miren binary: /bin/m (if built)"
echo ""
echo "Logs:"
echo "  - containerd: /tmp/containerd.log"
echo "  - buildkit:   /tmp/buildkit.log"
echo ""
echo "To attach: docker exec -it miren-dev bash"
echo ""

# Keep container alive
tail -f /dev/null
