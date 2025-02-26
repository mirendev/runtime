#!/bin/sh

set -x

rm data/cache/*
rm tmp/blah/*
go build -o bin/lsvd ./cmd/lsvd
./bin/lsvd volume init -c ./hack/minio.hcl -n ubuntu -s 50G
# ./bin/lsvd dd -c ./hack/minio.hcl -n ubuntu -p ./tmp/blah -i https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img

file=raw.img
count=1
seek=0
size=3758096384

./bin/lsvd sha256 -c ./hack/minio.hcl -n ubuntu -p ./tmp/blah -s $size --bs 50 --count $count --seek $seek

# dd if=$file bs=4096 | sha256sum #  count=$count skip=$seek | sha256sum

# echo "should be: 9ac187b2b662e544637f0f22530120b5ee4d11abda300000a539a6ebbcf8500d"
#echo "should be: 749eb9015e1cb492639a9a0b9e844bb1c42ffddb84bb6bc0c3af9f0c716a0906"
