# Miren Runtime Build Environment

FROM golang:1.25

# Install system dependencies
RUN apt-get update && apt-get install -y \
    bash \
    gotestsum \
    inetutils-ping \
    iproute2 \
    iptables \
    tmux \
    vim \
    netcat-openbsd \
    util-linux \
    e2fsprogs \
    less \
    file \
    htop \
    sudo \
    && rm -rf /var/lib/apt/lists/*

RUN if [ "$(uname -m)" = "aarch64" ]; then \
        echo "https://github.com/containerd/containerd/releases/download/v2.0.4/containerd-2.0.4-linux-arm64.tar.gz" > /tmp/containerd_url; \
        echo "https://github.com/moby/buildkit/releases/download/v0.17.1/buildkit-v0.17.1.linux-arm64.tar.gz" > /tmp/buildkit_url; \
        echo "https://github.com/opencontainers/runc/releases/download/v1.2.2/runc.arm64" > /tmp/runc_url; \
        echo "https://github.com/containerd/nerdctl/releases/download/v2.0.5/nerdctl-2.0.5-linux-arm64.tar.gz" > /tmp/nerdctl_url; \
    else \
        echo "https://github.com/containerd/containerd/releases/download/v2.0.4/containerd-2.0.4-linux-amd64.tar.gz" > /tmp/containerd_url; \
        echo "https://github.com/moby/buildkit/releases/download/v0.17.1/buildkit-v0.17.1.linux-amd64.tar.gz" > /tmp/buildkit_url; \
        echo "https://github.com/opencontainers/runc/releases/download/v1.2.2/runc.amd64" > /tmp/runc_url; \
        echo "https://github.com/containerd/nerdctl/releases/download/v2.0.5/nerdctl-2.0.5-linux-amd64.tar.gz" > /tmp/nerdctl_url; \
    fi && \
    mkdir -p /upstream && \
    curl -fL -o /upstream/containerd.tar.gz "$(cat /tmp/containerd_url)" && \
    curl -fL -o /upstream/buildkit.tar.gz "$(cat /tmp/buildkit_url)" && \
    curl -fL -o /upstream/nerdctl.tar.gz "$(cat /tmp/nerdctl_url)" && \
    curl -fL -o /upstream/runc "$(cat /tmp/runc_url)" && \
    chmod +x /upstream/runc && \
    tar -C /usr/local -xvf /upstream/containerd.tar.gz && \
    tar -C /usr/local -xvf /upstream/buildkit.tar.gz && \
    tar -C /usr/local/bin -xvf /upstream/nerdctl.tar.gz && \
    mv /upstream/runc /usr/local/bin/runc && \
    rm /tmp/*_url && \
    rm -rf /upstream

# Create containerd config directory
RUN mkdir -p /etc/containerd
COPY ./hack/containerd-config.toml /etc/containerd/config.toml

# Install bashrc for persistent shell configuration
COPY ./.iso/.bashrc /root/.bashrc
RUN chmod 755 /root && \
    chmod 644 /root/.bashrc && \
    mkdir -p /root/.cache && \
    chmod 777 /root/.cache && \
    touch /root/.cache/.bash_history && \
    chmod 666 /root/.cache/.bash_history

# Setup Go cache directories
ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/go/build-cache

# Set working directory
WORKDIR /workspace

# Ensure proper permissions for cache directories
RUN mkdir -p /go/pkg/mod /go/build-cache && \
    chmod -R 777 /go/pkg/mod /go/build-cache
