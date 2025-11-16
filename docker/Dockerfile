# Build stage
FROM golang:1.25 AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    git \
    make \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /build

# Copy source code
COPY . .

# Build miren
RUN make bin/miren

# Runtime stage
FROM ubuntu:24.04

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    bash \
    ca-certificates \
    iptables \
    iproute2 \
    netcat-openbsd \
    nftables \
    util-linux \
    && rm -rf /var/lib/apt/lists/*

# Copy miren binary from builder
COPY --from=builder /build/bin/miren /usr/local/bin/miren

# Copy entrypoint script
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Create necessary directories
RUN mkdir -p /var/lib/miren

# Download the components to run the server
RUN miren download release -g

# Declare volume for data persistence
VOLUME /var/lib/miren

# Expose ports
# 80 - HTTP
# 8443 - QUIC (UDP)
# 443 - HTTPS (TCP)
EXPOSE 80/tcp 8443/udp 443/tcp

ENTRYPOINT ["/entrypoint.sh"]

# Run miren server
CMD ["server", "-v"]
