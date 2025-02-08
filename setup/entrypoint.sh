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

mkdir -p /data /run /etc/miren

echo "run-containerd = true" > /etc/miren/server.conf
echo "data-path = \"/var/lib/miren\"" >> /etc/miren/server.conf

if test -n "$SERVER_ID"; then
  echo "id = \"$SERVER_ID\"" >> /etc/miren/server.conf
fi

if test -n "$INSECURE_ACCESS"; then
  echo "WARNING: INSECURE_ACCESS is set, allowing unauthenticated access to the server"
  echo "require-client-certs = false" >> /etc/miren/server.conf
else
  echo "require-client-certs = true" >> /etc/miren/server.conf
fi

if test -n "$DISABLE_LOCAL"; then
  echo "WARNING: DISABLE_LOCAL is set, disabling local access to the server"
else
  echo "local = \"/run/miren/miren.sock\"" >> /etc/miren/server.conf
fi

exec miren server -v -v --options /etc/miren/server.conf
