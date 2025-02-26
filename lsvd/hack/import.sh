#!/bin/sh

set -x

rm -rf ~/tmp/data/lsvdinttest/*
rm data/cache/*
rm tmp/blah/*
go build -o bin/lsvd ./cmd/lsvd || exit 1
./bin/lsvd volume init -c ./hack/minio.hcl -n ubuntu -s 50G
# ./bin/lsvd dd -c ./hack/minio.hcl -n ubuntu -p ./tmp/blah -i https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img
./bin/lsvd dd -c ./hack/minio.hcl -n ubuntu -p ./tmp/blah -i noble-server-cloudimg-arm64.img --expand --verify 9ac187b2b662e544637f0f22530120b5ee4d11abda300000a539a6ebbcf8500d --readback
