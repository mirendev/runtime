package compute

import (
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity/types"
)

func TestSandboxStatusCoverage(t *testing.T) {
	// Hibernation owns resources without advertising an active DNS endpoint.
	allStatuses := []compute_v1alpha.SandboxStatus{
		compute_v1alpha.PENDING,
		compute_v1alpha.NOT_READY,
		compute_v1alpha.RUNNING,
		compute_v1alpha.HIBERNATING,
		compute_v1alpha.HIBERNATED,
		compute_v1alpha.RESTORING,
		compute_v1alpha.STOPPED,
		compute_v1alpha.DEAD,
	}

	for _, s := range allStatuses {
		active := SandboxActive(s)
		dead := SandboxDead(s)
		hibernation := SandboxHibernation(s)

		if !active && !dead && !hibernation {
			t.Errorf("status %q is not classified", s)
		}
		if (active && dead) || (hibernation && (active || dead)) {
			t.Errorf("status %q belongs to multiple lifecycle classes", s)
		}
	}
}

// SandboxKind separates a user's own services from the platform underneath
// them. It lives in this package because the sandbox controller and the usage
// service both need the same answer; when they each had their own copy the two
// disagreed, so a --kind filter could select rows whose metrics said otherwise.
func TestSandboxKind(t *testing.T) {
	withAttrs := func(kv ...string) *compute_v1alpha.Sandbox {
		sb := &compute_v1alpha.Sandbox{}
		sb.Spec.LogAttribute = types.LabelSet(kv...)
		return sb
	}

	r := require.New(t)

	r.Equal(KindApp, SandboxKind(withAttrs("miren.stage", "app-run", "miren.service", "web")),
		"appspec stamps app-run on every deployed service")
	r.Equal(KindRun, SandboxKind(withAttrs("miren.stage", "run", "miren.task", "migrate")),
		"the run controller stamps run on one-off tasks")

	// Addon servers set no stage at all, so the addon label is the only signal
	// that a sandbox is infrastructure rather than someone's app.
	r.Equal(KindAddon, SandboxKind(withAttrs("addon", "postgresql", "app", "myapp")),
		"an addon is infrastructure even though it carries an app label")

	r.Equal(KindOther, SandboxKind(withAttrs()),
		"an unclassifiable sandbox is reported as such rather than assumed to be an app")
}
