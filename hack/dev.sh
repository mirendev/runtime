set -e

# Solve the issue of runsc not being able to manipulate subtree_control
# by moving everything here into a new cgroup so the root can be changed.

mkdir /sys/fs/cgroup/inner

for pid in "$(cat /sys/fs/cgroup/cgroup.procs)"; do
  echo "$pid" >/sys/fs/cgroup/inner/cgroup.procs 2>/dev/null || true
done

sed -e 's/ / +/g' -e 's/^/+/' </sys/fs/cgroup/cgroup.controllers >/sys/fs/cgroup/cgroup.subtree_control

mount -t debugfs nodev /sys/kernel/debug
mount -t tracefs nodev /sys/kernel/debug/tracing
mount -t tracefs nodev /sys/kernel/tracing

cd /src

make bin/runtime
ln -s "$PWD"/bin/runtime /bin/r

# Define runtime paths and arguments
RELEASE_PATH="/usr/local/bin"
DATA_PATH="/data/runtime"
mkdir -p "$DATA_PATH"
RUNTIME_ARGS="dev -vv --mode=standalone --release-path=$RELEASE_PATH --data-path=$DATA_PATH --skip-client-config"
RUNTIME_SERVER_CMD="./bin/runtime $RUNTIME_ARGS"

CONFIG_PATH="$HOME/.config/runtime/clientconfig.yaml"
mkdir -p "$(dirname "$CONFIG_PATH")"

r auth generate --data-path="$DATA_PATH" --cluster-name=dev --config-path="$CONFIG_PATH"

# Convenience config for development
export HISTFILE=/data/.bash_history
export HISTIGNORE=exit
export CONTAINERD_NAMESPACE=runtime
export CONTAINERD_ADDRESS="$DATA_PATH/containerd/containerd.sock"
export LESS="-fr"

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
  tmux send-keys -t dev:0.0 "$RUNTIME_SERVER_CMD" Enter
  tmux select-pane -t dev:0.1
  tmux attach-session -t dev
else
  # Start the server in the background
  eval "$RUNTIME_SERVER_CMD" >/tmp/server.log 2>&1 &
  echo "Server started, logs are in /tmp/server.log"

  # Start a shell
  bash
fi
