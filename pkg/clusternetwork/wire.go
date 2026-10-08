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
	// APIAddressDetails describes each entry of APIAddresses, in the same
	// order, with what the runtime knows about how a client reaches it.
	// It rides alongside the flat list rather than replacing it, so a
	// cloud that predates it ignores it.
	//
	// Deliberately not omitempty: a runtime that knows about details
	// always sends the field, as [] when it advertises nothing, so cloud
	// can tell "advertising nothing" from "predates details" by presence.
	// Never leave it nil, which would encode as null.
	APIAddressDetails []AdvertisedAddress `json:"api_address_details"`
	// CACertFingerprint is the hex SHA-1 of the DER CA certificate the API
	// presents, which the CLI pins when it connects directly.
	CACertFingerprint string `json:"ca_cert_fingerprint,omitempty"`
	// Reachability is netcheck's verdict, nil when it produced no usable
	// public source address; cloud then keeps whatever it last stored.
	Reachability *cloudauth.ReachabilityVerdict `json:"reachability,omitempty"`
	// Containerized is sent unconditionally so an explicit false lands.
	Containerized bool `json:"containerized"`
}

// TransportIP is the transport of an address a client dials as host:port.
// It is the only transport today; overlays that address peers by key rather
// than by IP (iroh) will arrive as further values, and a reader must skip an
// entry whose transport it doesn't know rather than reject the report.
const TransportIP = "ip"

// Class values say what network a client has to be on to reach an address.
const (
	// ClassPublic is reachable from anywhere on the internet.
	ClassPublic = "public"
	// ClassLAN is reachable from networks the host is attached to directly:
	// RFC 1918 space, a ULA, or CGNAT space on an ordinary link.
	ClassLAN = "lan"
	// ClassOverlay is reachable only from peers on a tunnel or overlay the
	// host has joined, such as a tailnet or a WireGuard mesh.
	ClassOverlay = "overlay"
	// ClassContainerBridge is reachable only from workloads on this host.
	ClassContainerBridge = "container-bridge"
)

// Range values are the address's place in the IANA registries, a pure
// function of the IP. Class is the runtime's judgment; Range is the fact
// it started from.
const (
	RangePublic  = "public"
	RangePrivate = "private" // RFC 1918
	RangeShared  = "shared"  // RFC 6598, carrier-grade NAT and most overlays
	RangeULA     = "ula"     // RFC 4193
	RangeSpecial = "special" // other IANA special-purpose space
)

// Source values say how the runtime came to know an address.
const (
	SourceListen     = "listen"
	SourceExplicit   = "explicit"
	SourceDiscovered = "discovered"
	SourceNetcheck   = "netcheck"
)

// AdvertisedAddress is one advertised API endpoint and what the runtime
// knows about it.
type AdvertisedAddress struct {
	Transport string `json:"transport"`
	// Address is "host:port" for TransportIP, the same string that appears
	// in Report.APIAddresses.
	Address string `json:"address"`
	Class   string `json:"class"`
	Range   string `json:"range,omitempty"`
	// Interface is the local link the address was found on, empty when the
	// runtime didn't find it on one (configured, observed by netcheck).
	Interface string `json:"interface,omitempty"`
	Source    string `json:"source"`
	// Label names the vendor or technology behind the address when it is
	// recognizable ("tailscale", "zerotier", "netbird", "wireguard"). It is
	// a hint for people and never what Class was decided from.
	Label string `json:"label,omitempty"`
}
