// Package session defines a durable workload identity whose Sandbox executions
// may be replaced without changing the identity.
package session

import (
	"fmt"

	"miren.dev/runtime/pkg/entity"
)

//go:generate go run ../../pkg/entity/cmd/schemagen -input schema.yml -output session_v1alpha/schema.gen.go -pkg session_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg session_v1alpha -input rpc.yml -output session_v1alpha/rpc.gen.go

// BindingID is stable across coordinator restarts and retains a deleted
// Session's identity until its sandbox acknowledges the deletion.
func BindingID(session entity.Id) entity.Id {
	return entity.Id("session_binding/" + session.PathSafe())
}

// SlotID is the create-if-absent capacity reservation in a managed host.
func SlotID(sandbox entity.Id, index int64) entity.Id {
	return entity.Id(fmt.Sprintf("session_slot/%s/%d", sandbox.PathSafe(), index))
}
