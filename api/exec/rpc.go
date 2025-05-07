package exec

//go:generate mkdir -p exec_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg exec_v1alpha -input rpc.yml -output exec_v1alpha/rpc.gen.go
