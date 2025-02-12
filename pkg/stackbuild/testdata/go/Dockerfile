# syntax=docker/dockerfile:1

FROM golang:1.23-alpine

RUN apk add --no-cache clang

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod go mod download -json

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod go build -o /go/bin/blah .
