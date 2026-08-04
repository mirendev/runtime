package workloadidentity

import (
	"fmt"
)

// SystemWorkload names a Miren-owned workload that may receive an identity.
// System workloads are a closed set: adding one is a code change, not a
// runtime configuration operation.
type SystemWorkload string

const (
	SystemWorkloadSandboxController SystemWorkload = "sandboxcontroller"
	SystemWorkloadTelemetryWriter   SystemWorkload = "telemetrywriter"
	SystemWorkloadBuildKit          SystemWorkload = "buildkit"
)

// ParseSystemWorkload converts the string representation used on the wire into a
// known system workload.
func ParseSystemWorkload(value string) (SystemWorkload, error) {
	workload := SystemWorkload(value)
	if !workload.valid() {
		return "", fmt.Errorf("unknown system workload %q", value)
	}
	return workload, nil
}

func (w SystemWorkload) valid() bool {
	switch w {
	case SystemWorkloadSandboxController, SystemWorkloadTelemetryWriter, SystemWorkloadBuildKit:
		return true
	default:
		return false
	}
}

// IssueSystemWorkloadToken mints a token identifying a Miren-owned workload
// (the sandbox controller, a telemetry writer) rather than a customer workload.
//
// System workload tokens let Miren's own code authenticate to cluster-internal
// services without a second credential system: they are minted by the same
// issuer, verified the same way, and inherit the short lifetimes that make
// revocation tractable. They are deliberately distinguishable from sandbox
// tokens by the identity_type claim, because the services they open are
// precisely the ones a sandbox must not reach.
//
// Callers choose an audience scoped to the service they intend to reach, since
// one workload may call several services. The receiving service verifies that
// audience and the expected workload together so a token minted for one
// workload or service cannot be replayed against another.
func (iss *Issuer) IssueSystemWorkloadToken(workload SystemWorkload, opts TokenOptions) (string, error) {
	if !workload.valid() {
		return "", fmt.Errorf("unknown system workload %q", workload)
	}

	subject, err := newSystemWorkloadSubject(iss.organizationID, iss.clusterID, workload)
	if err != nil {
		return "", fmt.Errorf("building system workload subject: %w", err)
	}

	claims := iss.baseClaims(subject, opts)
	claims.SystemWorkload = workload
	claims.IdentityType = IdentityTypeSystem

	return iss.sign(claims)
}
