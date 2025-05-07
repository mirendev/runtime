package build

//go:generate mkdir -p build_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg build_v1alpha -input rpc.yml -output build_v1alpha/rpc.gen.go
