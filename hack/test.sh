set -e

# Solve the issue of runsc not being able to manipulate subtree_control
# by moving everything here into a new cgroup so the root can be changed.

mkdir /sys/fs/cgroup/inner

cat /sys/fs/cgroup/cgroup.procs | while read -r pid; do
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

# Since our buildkit dir is cached across runs, there might be a stale lockfile
# sitting around that should be safe to kill
rm -f /data/buildkit/buildkitd.lock
buildkitd --root /data/buildkit 2>&1 &

mount -t debugfs nodev /sys/kernel/debug
mount -t tracefs nodev /sys/kernel/debug/tracing
mount -t tracefs nodev /sys/kernel/tracing

# Wait for containerd and buildkitd to start
echo "Waiting for containerd..."
timeout=30
count=0
while ! ctr --address /run/containerd/containerd.sock version >/dev/null 2>&1; do
  sleep 1
  count=$((count + 1))
  if [ "$count" -ge "$timeout" ]; then
    echo "Timeout waiting for containerd"
    exit 1
  fi
done
echo "containerd is ready"

# Wait for buildkitd with timeout
echo "Waiting for buildkitd..."
timeout=30
count=0
while ! buildctl debug info >/dev/null 2>&1; do
  sleep 1
  count=$((count + 1))
  if [ "$count" -ge "$timeout" ]; then
    echo "Timeout waiting for buildkitd"
    exit 1
  fi
done
echo "buildkitd is ready"

cd /src

if test "$USESHELL" != ""; then
  export HISTFILE=/data/.bash_history
  export HISTIGNORE=exit
  bash
# Because all the tests use the same containerd, buildkit, and cgroups, we need to
# make sure that they don't interfere with each other. For now, we do that by passing
# -p 1, but in the future we should run each test in a separate namespace.
elif test "$VERBOSE" != ""; then
  go test -p 1 -v "$@"
else
  gotestsum --format testname -- -p 1 "$@"
fi
