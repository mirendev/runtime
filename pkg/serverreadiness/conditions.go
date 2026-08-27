// Package serverreadiness defines the shared vocabulary between the server
// boot graph and code that waits for it. The boot graph decides which
// components satisfy each condition; consumers only depend on the names.
package serverreadiness

import "miren.dev/runtime/pkg/readiness"

var (
	// BuildReady holds when the system can build and publish application artifacts.
	BuildReady = readiness.NewCondition("build")
	// SandboxesReady holds when the system can launch application sandboxes.
	SandboxesReady = readiness.NewCondition("sandboxes")
	// ServeReady holds when the system can accept and route application traffic.
	ServeReady = readiness.NewCondition("serve")
)
