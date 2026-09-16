package clusternetwork

// This file is the runtime half of the cluster-network wire contract. Its
// cloud counterpart is mirendev/cloud/services/clusternetwork/wire.go. Keep
// the JSON shapes in lockstep until the uplink has a shared schema home.

import "miren.dev/runtime/pkg/cloudauth"

const (
	Version1 uint = 1

	TypeReport = "cluster.network.report"
)

// Report is what this cluster says about how it can be reached. It is the
// network-shaped half of the legacy status report, field for field, so cloud
// treats the two sources identically while both are live.
//
// There is no cluster field: cloud takes identity from the authenticated
// socket, never from the payload.
type Report struct {
	// APIAddresses are the "host:port" endpoints advertised for the API,
	// already pruned to what netcheck confirmed (see ComputeAdvertise).
	APIAddresses []string `json:"api_addresses,omitempty"`
	// CACertFingerprint is the hex SHA-1 of the DER CA certificate the API
	// presents, which the CLI pins when it connects directly.
	CACertFingerprint string `json:"ca_cert_fingerprint,omitempty"`
	// Reachability is netcheck's verdict, nil when it produced no usable
	// public source address; cloud then keeps whatever it last stored.
	Reachability *cloudauth.ReachabilityVerdict `json:"reachability,omitempty"`
	// Containerized is sent unconditionally so an explicit false lands.
	Containerized bool `json:"containerized"`
}
