mkdir -p /data /run

# Compile in the background while containerd starts
go build -o /bin/containerd-log-ingress ./run/containerd-log-ingress &

containerd --root /data --state /data/state --address /run/containerd.sock -l trace &
buildkitd --root /data/buildkit &

# Wait for containerd and buildkitd to start
sleep 1

cd /src

gotestsum --format testname "$@"
