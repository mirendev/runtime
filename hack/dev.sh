#!/usr/bin/env bash
set -e

# Source common setup functions
source "$(dirname "$0")/common-setup.sh"

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

# Wait for containerd to start (simpler wait for dev)
sleep 1

cd /src

# Wait for services to be ready (Dagger service bindings)
echo "Checking service connectivity..."
for service in etcd clickhouse minio; do
  echo -n "Waiting for $service..."
  count=0
  while ! ping -c 1 -W 1 $service >/dev/null 2>&1; do
    echo -n "."
    sleep 1
    count=$((count + 1))
    if [ "$count" -ge 30 ]; then
      echo " FAILED"
      echo "ERROR: Could not reach $service after 30 seconds"
      echo "Checking DNS resolution:"
      getent hosts $service || echo "DNS lookup failed for $service"
      break
    fi
  done
  if [ "$count" -lt 30 ]; then
    echo " OK"
  fi
done

# Also check if services are actually responding
echo "\nChecking service endpoints:"
echo -n "ClickHouse native port: "
nc -zv clickhouse 9000 2>&1 || echo "not reachable"
echo -n "Etcd client port: "
nc -zv etcd 2379 2>&1 || echo "not reachable"
echo -n "MinIO port: "
nc -zv minio 9000 2>&1 || echo "not reachable"
echo ""

# Build runtime
make bin/runtime

# Create symlink
ln -s "$PWD"/bin/runtime /bin/r

# Setup runtime config
mkdir -p ~/.config/runtime
r auth generate -c ~/.config/runtime/clientconfig.yaml

echo "Cleaning runtime namespace to begin..."
r debug ctr nuke -n runtime --containerd-socket "$CONTAINERD_ADDRESS"

# Setup environment variables
setup_bash_environment

if [[ -n "$USE_TMUX" ]]; then
  # Make a tmux session for us to run multiple shells in
  tmux new-session -d -s dev

  # Set the prefix to one that is unlikely to overlap: ctrl-s
  tmux unbind-key C-b
  tmux set-option -g prefix C-s
  tmux bind-key C-s send-prefix

  # Some quality of life settings
  tmux set-option -g mode-keys vi

  # Start with two panes with the server running on top and a shell running on the bottom
  tmux split-window -v
  tmux send-keys -t dev:0.0 "./bin/runtime server -vv --mode=distributed" Enter
  tmux select-pane -t dev:0.1
  tmux attach-session -t dev
else
  # Start the server in the background
  ./bin/runtime server -vv --mode=distributed >/tmp/server.log 2>&1 &
  echo "Server started, logs are in /tmp/server.log"

  # Start a shell
  bash
fi
