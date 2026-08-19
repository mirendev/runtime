package ingress

//go:generate go run ../../pkg/entity/cmd/schemagen -input schema.yml -output ingress_v1alpha/schema.gen.go -pkg ingress_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg ingress_v1alpha -input rpc.yml -output ingress_v1alpha/rpc.gen.go
