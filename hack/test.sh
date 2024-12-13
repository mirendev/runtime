set -e

# Solve the issue of runsc not being able to manipulate subtree_control
# by moving everything here into a new cgroup so the root can be changed.

mkdir /sys/fs/cgroup/inner

for pid in $(cat /sys/fs/cgroup/cgroup.procs); do
  echo $pid > /sys/fs/cgroup/inner/cgroup.procs 2>/dev/null || true
done

sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers > /sys/fs/cgroup/cgroup.subtree_control

mkdir -p /data /run

# Compile in the background while containerd starts
go build -o /bin/containerd-log-ingress ./run/containerd-log-ingress &

containerd --root /data --state /data/state --address /run/containerd.sock -l trace > /dev/null 2>&1 &
buildkitd --root /data/buildkit > /dev/null 2>&1 &

mount -t debugfs nodev /sys/kernel/debug
mount -t tracefs nodev /sys/kernel/debug/tracing
mount -t tracefs nodev /sys/kernel/tracing

# Wait for containerd and buildkitd to start
sleep 1

cd /src

# Because all the tests use the same containerd, buildkit, and cgroups, we need to
# make sure that they don't interfere with each other. For now, we do that by passing
# -p 1, but in the future we should run each test in a separate namespace.
gotestsum --format testname -- -p 1 "$@"
