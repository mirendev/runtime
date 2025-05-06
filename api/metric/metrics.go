package metric

//go:generate mkdir -p metric_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg metric_v1alpha -input rpc.yml -output metric_v1alpha/rpc.gen.go
