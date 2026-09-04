package compute

import "miren.dev/runtime/api/compute/compute_v1alpha"

//go:generate go run ../../pkg/entity/cmd/schemagen -input schema.yml -output compute_v1alpha/schema.gen.go -pkg compute_v1alpha

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

// Kind classifies what a sandbox is for.
type Kind string

const (
	// KindApp is a sandbox running a deployed application service.
	KindApp Kind = "app"
	// KindAddon is managed infrastructure an app depends on, such as a
	// dedicated database, or a shared server no single app owns.
	KindAddon Kind = "addon"
	// KindRun is a one-off task run.
	KindRun Kind = "run"
	// KindOther is a sandbox none of the above describe.
	KindOther Kind = "other"
)

// SandboxKind separates a user's own services from the platform running
// underneath them, so a usage report can tell a web process from the database
// sitting behind it.
//
// This lives here rather than beside either caller because two of them need the
// same answer: the sandbox controller stamps it onto every metric series, and
// the usage service filters rows by it. When they were separate implementations
// they disagreed, which meant a --kind filter could select rows whose metrics
// said something else.
//
// The classification is read back out of attributes the sandbox already
// carries rather than stored on it: appspec stamps miren.stage=app-run on
// deployed services and the run controller stamps miren.stage=run, while the
// addon framework passes the addon's own labels through as log attributes and
// sets no stage.
func SandboxKind(sb *compute_v1alpha.Sandbox) Kind {
	var stage string

	for _, lbl := range sb.Spec.LogAttribute {
		switch lbl.Key {
		case "addon":
			return KindAddon
		case "miren.stage":
			stage = lbl.Value
		}
	}

	switch stage {
	case "run":
		return KindRun
	case "app-run":
		return KindApp
	}

	return KindOther
}
