// Package nodeadmin carries the RPC the coordinator uses to ask one node to
// change something about itself.
package nodeadmin

//go:generate mkdir -p nodeadmin_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg nodeadmin_v1alpha -input rpc.yml -output nodeadmin_v1alpha/rpc.gen.go
