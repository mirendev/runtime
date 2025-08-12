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

# Wait for services to be ready using the common helper
wait_for_service "etcd" "nc -z etcd 2379"
wait_for_service "clickhouse" "nc -z clickhouse 9000"
wait_for_service "minio" "nc -z minio 9000"

# Build miren
make bin/miren

# Create symlink
ln -s "$PWD"/bin/miren /bin/m

# Setup runtime config
mkdir -p ~/.config/runtime
m auth generate -c ~/.config/runtime/clientconfig.yaml

echo "Cleaning runtime namespace to begin..."
m debug ctr nuke -n runtime --containerd-socket "$CONTAINERD_ADDRESS"

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
  tmux send-keys -t dev:0.0 "./bin/miren server -vv --mode=distributed" Enter
  tmux select-pane -t dev:0.1
  tmux attach-session -t dev
else
  # Start the server in the background
  ./bin/miren server -vv --mode=distributed >/tmp/server.log 2>&1 &
  echo "Server started, logs are in /tmp/server.log"

  # Start a shell
  bash
fi
