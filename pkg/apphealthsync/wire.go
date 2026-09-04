package apphealthsync

import "time"

const (
	Version1 uint = 1

	TypeSamples = "app.health.samples"
	TypeDemand  = "app.health.demand"
)

// Sample is one app's health as the runtime classifies it.
//
// There is no cluster or organization field. Cloud derives both from the
// authenticated identity on the socket: a cluster is never trusted to say
// which tenant its data belongs to, only what its own apps are doing.
type Sample struct {
	Name string `json:"name"`

	// Health is one of the apphealth values. It is computed by the same code
	// that answers `miren app list`, so the CLI and the console cannot
	// disagree about whether an app is fine.
	Health string `json:"health"`

	ReadyInstances   int32 `json:"ready_instances"`
	DesiredInstances int32 `json:"desired_instances"`
}

// SampleBatch is one message's worth of samples.
//
// They share one ObservedAt because they come from a single derivation of the
// cluster's state. Cloud applies last-writer-wins with it, so a message
// delayed behind a fresher one cannot reinstate health that was already
// corrected.
type SampleBatch struct {
	ObservedAt time.Time `json:"observed_at"`
	Apps       []Sample  `json:"apps"`
}

// Config supplies the quiet-cluster cadence when the session opens.
type Config struct {
	BackgroundSeconds int `json:"background_seconds"`
}

// Demand temporarily accelerates sampling for a viewed cluster. SessionID
// prevents a queued request from activating a replacement connection.
type Demand struct {
	SessionID       string `json:"session_id"`
	IntervalSeconds int    `json:"interval_seconds"`
	LeaseSeconds    int    `json:"lease_seconds"`
}
