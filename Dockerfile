# syntax=docker/dockerfile:1

FROM alpine:latest AS binaries

ADD https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/runsc /data/
ADD https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/containerd-shim-runsc-v1 /data/

FROM golang:1.24-alpine AS builder

RUN apk add --no-cache clang

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=1 go build -o /go/bin/runtime ./cmd/runtime
RUN --mount=type=cache,target=/root.cache/go-build go build -o /go/bin/containerd-log-ingress ./run/containerd-log-ingress

FROM alpine:latest AS base

RUN apk add --no-cache containerd nerdctl iptables curl

COPY --chmod=0755 --from=binaries /data/runsc /usr/local/bin/runsc
COPY --chmod=0755 --from=binaries /data/containerd-shim-runsc-v1 /usr/local/bin/containerd-shim-runsc-v1

FROM base AS app

COPY --from=builder /go/bin/runtime /bin/runtime
COPY --from=builder /go/bin/containerd-log-ingress /bin/containerd-log-ingress

COPY --from=builder /app/setup/entrypoint.sh /entrypoint.sh

COPY --from=builder /app/db /db

COPY --from=builder --chmod=0755 /app/setup/runsc-runtime /bin/runsc-runtime

RUN chmod +x /entrypoint.sh

VOLUME /var/lib/runtime

ENTRYPOINT ["/entrypoint.sh"]
