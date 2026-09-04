package disk

//go:generate mkdir -p disk_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg disk_v1alpha -input rpc.yml -output disk_v1alpha/rpc.gen.go
