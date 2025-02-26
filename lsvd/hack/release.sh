#!/bin/sh

set -x

rm -rf ~/tmp/data/lsvdinttest/*
rm data/cache/*
go build -o bin/lsvd ./cmd/lsvd
./bin/lsvd volume init -c ./hack/minio.hcl -n bench -s 50G
./bin/lsvd nbd -c ./hack/minio.hcl -n bench -p ./data/cache
