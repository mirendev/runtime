# syntax=docker/dockerfile:1

FROM alpine:latest AS binaries

ADD https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/runsc /data/
ADD https://storage.googleapis.com/gvisor/releases/release/latest/aarch64/containerd-shim-runsc-v1 /data/

FROM golang:1.23-alpine AS builder

RUN apk add --no-cache clang

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o /go/bin/miren ./cmd/miren
RUN go build -o /go/bin/containerd-log-ingress ./run/containerd-log-ingress

FROM alpine:latest AS base

RUN apk add --no-cache containerd nerdctl iptables

COPY --chmod=0755 --from=binaries /data/runsc /usr/local/bin/runsc
COPY --chmod=0755 --from=binaries /data/containerd-shim-runsc-v1 /usr/local/bin/containerd-shim-runsc-v1

FROM base AS app

COPY --from=builder /go/bin/miren /bin/miren
COPY --from=builder /go/bin/containerd-log-ingress /bin/containerd-log-ingress

COPY --from=builder /app/setup/entrypoint.sh /entrypoint.sh

COPY --from=builder /app/db /db

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
