set -e

# Solve the issue of runsc not being able to manipulate subtree_control
# by moving everything here into a new cgroup so the root can be changed.

mkdir /sys/fs/cgroup/inner

for pid in "$(cat /sys/fs/cgroup/cgroup.procs)"; do
  echo "$pid" >/sys/fs/cgroup/inner/cgroup.procs 2>/dev/null || true
done

sed -e 's/ / +/g' -e 's/^/+/' </sys/fs/cgroup/cgroup.controllers >/sys/fs/cgroup/cgroup.subtree_control

mkdir -p /data /run

export OTEL_SDK_DISABLED=true

cat <<EOF >/etc/containerd/config.toml
version = 2
[plugins."io.containerd.runtime.v1.linux"]
  shim_debug = true
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.miren]
  runtime_type = "io.containerd.runsc.v1"

[metrics]
  address = "127.0.0.1:1338"
EOF

cat <<EOF >/etc/containerd/runsc.toml
log_path = "/var/log/runsc/%ID%/shim.log"
log_level = "debug"
binary_name = "/src/hack/runsc-ignore"
[runsc_config]
  debug = "true"
  debug-log = "/var/log/runsc/%ID%/gvisor.%COMMAND%.log"
EOF

mkdir -p /run/containerd
containerd --root /data --state /data/state --address /run/containerd/containerd.sock -l trace >/dev/null 2>&1 &

# Handy to build stuff with.
buildkitd --root /data/buildkit >/dev/null 2>&1 &

mount -t debugfs nodev /sys/kernel/debug
mount -t tracefs nodev /sys/kernel/debug/tracing
mount -t tracefs nodev /sys/kernel/tracing

# Wait for containerd and buildkitd to start
sleep 1

cd /src

make bin/runtime

ln -s "$PWD"/bin/runtime /bin/r

mkdir -p ~/.config/runtime

r auth generate -c ~/.config/runtime/clientconfig.yaml

echo "Cleaning runtime namespace to begin..."
r debug ctr nuke -n runtime

export HISTFILE=/data/.bash_history
export HISTIGNORE=exit
export CONTAINERD_NAMESPACE=runtime

if [[ -n "$USE_TMUX" ]]; then
  # Make a tmux session for us to run multiple shells in
  tmux new-session -d -s dev

  # Set the prefix to one that is unlikely to overlap: ctrl-s
  tmux unbind-key C-b
  tmux set-option -g prefix C-s
  tmux bind-key C-s send-prefix

  # Start with two panes with the server running on top and a shell running on the bottom
  tmux split-window -v
  tmux send-keys -t dev:0.0 "./bin/runtime dev -vv" Enter
  tmux select-pane -t dev:0.1
  tmux attach-session -t dev
else
  # Start the server in the background
  ./bin/runtime dev -vv > /tmp/server.log 2>&1 &
  echo "Server started, logs are in /tmp/server.log"

  # Start a shell
  bash
fi
