#!/bin/sh

set -x

rm -rf ~/tmp/data/lsvdinttest/*
rm data/cache/*
go build -tags debug -o bin/lsvd ./cmd/lsvd
./bin/lsvd volume init -c ./hack/minio.hcl -n bench -s 50G
./bin/lsvd nbd -D -c ./hack/minio.hcl -n bench -p ./data/cache
