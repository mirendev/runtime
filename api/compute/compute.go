package compute

import "miren.dev/runtime/api/compute/compute_v1alpha"

//go:generate go run ../../pkg/entity/cmd/schemagen -input schema.yml -output compute_v1alpha/schema.gen.go -pkg compute_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg compute_v1alpha -input rpc.yml -output compute_v1alpha/rpc.gen.go

// SandboxActive reports whether a sandbox status indicates the sandbox
// may be actively running (PENDING, NOT_READY, or RUNNING).
func SandboxActive(status compute_v1alpha.SandboxStatus) bool {
	return status == compute_v1alpha.PENDING || status == compute_v1alpha.NOT_READY || status == compute_v1alpha.RUNNING
}

// SandboxDead reports whether a sandbox status indicates the sandbox
// has stopped or failed (STOPPED or DEAD).
func SandboxDead(status compute_v1alpha.SandboxStatus) bool {
	return status == compute_v1alpha.STOPPED || status == compute_v1alpha.DEAD
}
