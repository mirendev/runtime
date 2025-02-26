#!/bin/sh

set -x

rm -rf ~/tmp/data/lsvdinttest/*
rm data/cache/*
go build -tags debug -gcflags="all=-N -l" -o bin/lsvd ./cmd/lsvd
./bin/lsvd volume init -c ./hack/minio.hcl -n bench -s 50G
dlv exec ./bin/lsvd -- nbd -c ./hack/minio.hcl -n bench -p ./data/cache
