mkdir -p /data /run

containerd --root /data --state /data/state --address /run/containerd.sock -l trace &
buildkitd --root /data/buildkit &

# Wait for containerd and buildkitd to start
sleep 1

cd /src

go build -o /bin/containerd-log-ingress ./run/containerd-log-ingress
gotestsum --format testname ./...
