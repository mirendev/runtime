package lifecyclesync

import (
	"miren.dev/runtime/pkg/serverlifecycle"
	"miren.dev/runtime/pkg/uplink"
)

// This file is the runtime half of the server-lifecycle wire contract. Its
// cloud counterpart is mirendev/cloud/services/serverlifecycle/wire.go. Keep
// the JSON shapes in lockstep.

const (
	Capability = uplink.CapabilityServerLifecycle

	Version1 uint = 1

	// TypeRequest asks the runtime to start an operation. Cloud mints the id,
	// so a request repeated after a lost reply finds its record already there.
	TypeRequest = "lifecycle.request"
	// TypeReject answers a request the runtime would not record.
	TypeReject = "lifecycle.reject"
	// TypeSync is sent once per session: everything cloud should know about
	// this ledger before status updates start flowing.
	TypeSync = "lifecycle.sync"
	// TypeStatus carries one record each time it changes.
	TypeStatus = "lifecycle.status"
)

// Offer is the capability payload in the session hello.
type Offer struct {
	Actions           []string `json:"actions"`
	RuntimeInstanceID string   `json:"runtime_instance_id"`
	// InstallKind says how the server is supervised (systemd, container,
	// unknown). Only a systemd install can be restarted or upgraded this way,
	// and cloud is better placed to say so before a request than after.
	InstallKind string `json:"install_kind,omitempty"`
}

// Config is cloud's selection payload in the session welcome.
type Config struct {
	// Watch names operations cloud still considers open. The sync reports each
	// of them, as a record or as missing, so cloud can settle them.
	Watch []string `json:"watch,omitempty"`
	// Actions echoes the offered action set as cloud accepted it. Cloud keeps
	// this config as the session's record of the capability and checks
	// requests against it; the runtime has nothing to do with it here.
	Actions []string `json:"actions,omitempty"`
}

// Request is cloud asking for one operation.
type Request struct {
	OperationID         string `json:"operation_id"`
	Action              string `json:"action"`
	TargetVersion       string `json:"target_version,omitempty"`
	ArtifactType        string `json:"artifact_type,omitempty"`
	NoRollback          bool   `json:"no_rollback,omitempty"`
	ReadyTimeoutSeconds int    `json:"ready_timeout_seconds,omitempty"`
	// RequestedBy is recorded verbatim on the operation.
	RequestedBy string `json:"requested_by,omitempty"`
}

// Reject says why a request was not recorded. A request that was recorded
// but then failed is not rejected; its record says what happened.
type Reject struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason"`
}

// Sync is the ledger as of session start: every operation still running,
// the most recent finished ones, and whichever watched ids exist. Watched ids
// that do not exist are listed as missing, which cloud reads as "this never
// reached the host".
type Sync struct {
	RuntimeInstanceID string                       `json:"runtime_instance_id"`
	Operations        []*serverlifecycle.Operation `json:"operations"`
	Missing           []string                     `json:"missing,omitempty"`
}

// Status is one record after a change.
type Status struct {
	RuntimeInstanceID string                     `json:"runtime_instance_id"`
	Operation         *serverlifecycle.Operation `json:"operation"`
}
