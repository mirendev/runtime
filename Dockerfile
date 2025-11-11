# syntax=docker/dockerfile:1

FROM ghcr.io/mirendev/runsc:latest AS binaries

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache clang

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /go/bin/miren ./cmd/miren
RUN --mount=type=cache,target=/root/.cache/go-build go build -trimpath -ldflags="-s -w" -o /go/bin/containerd-log-ingress ./run/containerd-log-ingress

FROM alpine:latest AS base

RUN apk add --no-cache containerd nerdctl iptables curl

COPY --chmod=0755 --from=binaries /runsc /usr/local/bin/runsc
COPY --chmod=0755 --from=binaries /containerd-shim-runsc-v1 /usr/local/bin/containerd-shim-runsc-v1

FROM base AS app

COPY --from=builder /go/bin/miren /bin/miren
COPY --from=builder /go/bin/containerd-log-ingress /bin/containerd-log-ingress

COPY --from=builder /app/setup/entrypoint.sh /entrypoint.sh

COPY --from=builder /app/db /db

COPY --from=builder --chmod=0755 /app/setup/runsc-miren /bin/runsc-miren

RUN chmod +x /entrypoint.sh

VOLUME /var/lib/miren

ENTRYPOINT ["/entrypoint.sh"]
