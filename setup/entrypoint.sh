#!/bin/sh

# Solve the issue of runsc not being able to manipulate subtree_control
# by moving everything here into a new cgroup so the root can be changed.

mkdir /sys/fs/cgroup/inner

for pid in $(cat /sys/fs/cgroup/cgroup.procs); do
  echo $pid > /sys/fs/cgroup/inner/cgroup.procs 2>/dev/null || true
done

sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers > /sys/fs/cgroup/cgroup.subtree_control

mount -t debugfs nodev /sys/kernel/debug
mount -t tracefs nodev /sys/kernel/debug/tracing
mount -t tracefs nodev /sys/kernel/tracing

mkdir -p /data /run

exec miren server -v -v --run-containerd
