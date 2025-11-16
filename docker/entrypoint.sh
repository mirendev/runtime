#!/bin/bash
set -e

if [ "$1" = "server" ]; then
    if [ -d /sys/fs/cgroup/inner ]; then
        # Already set up
        return
    fi

    # Solve the issue of not being able to manipulate subtree_control
    # by moving everything here into a new cgroup so the root can be changed.
    mkdir /sys/fs/cgroup/inner

    cat /sys/fs/cgroup/cgroup.procs | while read -r pid; do
        echo "$pid" >/sys/fs/cgroup/inner/cgroup.procs 2>/dev/null || true
    done

    sed -e 's/ / +/g' -e 's/^/+/' </sys/fs/cgroup/cgroup.controllers >/sys/fs/cgroup/cgroup.subtree_control
fi

exec /usr/local/bin/miren "$@"
