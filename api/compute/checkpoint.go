package compute

import (
	"strings"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/secret"
)

// CheckpointRetention bounds retention of process memory and writable snapshots.
const CheckpointRetention = time.Hour

// SandboxWaking consumes pool capacity without being ready to serve requests.
func SandboxWaking(status compute.SandboxStatus) bool {
	return status == compute.PENDING || status == compute.RESTORING
}

func SandboxHibernation(status compute.SandboxStatus) bool {
	return status == compute.HIBERNATING || status == compute.HIBERNATED || status == compute.RESTORING
}

// CheckpointEligible deliberately excludes resources whose consistency cannot
// be preserved by retaining a single container's private writable snapshot.
// Runtime capability and auto-idle policy are checked by their respective owners.
func CheckpointEligible(sb *compute.Sandbox) bool {
	if SandboxKind(sb) != KindApp || sb.Spec.Version == "" ||
		sb.Spec.HostNetwork || len(sb.Spec.Container) != 1 ||
		len(sb.Spec.Volume) != 0 || len(sb.Volume) != 0 ||
		sb.Spec.RestartPolicy == compute.SandboxSpecNEVER {
		return false
	}
	c := sb.Spec.Container[0]
	if c.Privileged || c.Tty || c.Stdin || len(c.Mount) != 0 || len(c.ConfigFile) != 0 {
		return false
	}
	for _, env := range c.Env {
		name, value, _ := strings.Cut(env, "=")
		// An OCI env update cannot change a checkpointed process's memory.
		// These credentials are injected independently of user config and may
		// have expired or been rotated while the process was asleep.
		if name == "ADMIN_TOKEN" || strings.HasPrefix(name, "MIREN_IDENTITY_") ||
			strings.HasPrefix(value, secret.SentinelScheme) {
			return false
		}
	}
	for _, p := range c.Port {
		if p.NodePort != 0 || p.Protocol == compute.SandboxSpecContainerPortUDP {
			return false
		}
	}
	return true
}
