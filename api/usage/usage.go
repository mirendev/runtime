package usage

//go:generate mkdir -p usage_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg usage_v1alpha -input rpc.yml -output usage_v1alpha/rpc.gen.go
