# syntax=docker/dockerfile:1

FROM scratch AS src

ADD https://github.com/containerd/containerd/releases/download/v2.0.0/containerd-2.0.0-linux-arm64.tar.gz /containerd.tar.gz

FROM ubuntu:22.04

RUN --mount=from=src,target=/src <<EOF
tar -oxzf /src/containerd.tar.gz -C /
EOF

