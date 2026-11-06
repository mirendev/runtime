package certrevocation

//go:generate mkdir -p certrevocation_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg certrevocation_v1alpha -input rpc.yml -output certrevocation_v1alpha/rpc.gen.go
