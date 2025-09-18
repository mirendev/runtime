#!/usr/bin/env bash
set -e

# Source common setup functions
source "$(dirname "$0")/common-setup.sh"

# Setup environment
setup_cgroups
setup_environment

# In standalone mode, miren server manages its own containerd
# so we don't need to start external containerd or buildkitd
# Export the address for convenience when using tools like ctr
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

# Copy binaries to release directory for standalone mode
echo "Setting up release directory for standalone mode..."
mkdir -p /var/lib/miren/release
cp bin/miren /var/lib/miren/release/
cp /usr/local/bin/runc /var/lib/miren/release/
cp /usr/local/bin/containerd-shim-runsc-v1 /var/lib/miren/release/
cp /usr/local/bin/containerd-shim-runc-v2 /var/lib/miren/release/
cp /usr/local/bin/containerd /var/lib/miren/release/
cp /usr/local/bin/nerdctl /var/lib/miren/release/
cp /usr/local/bin/ctr /var/lib/miren/release/
echo "Release directory setup complete"

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
  tmux send-keys -t dev:0.0 "./bin/miren server -vv --mode standalone" Enter
  tmux select-pane -t dev:0.1
  tmux attach-session -t dev
else
  # Start the server in the background
  ./bin/miren server -vv --mode standalone >/tmp/server.log 2>&1 &
  echo "Server started in standalone mode, logs are in /tmp/server.log"

  # Start a shell
  bash
fi