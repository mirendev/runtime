package compute

import (
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity/types"
)

func TestSandboxStatusCoverage(t *testing.T) {
	// Every SandboxStatus must be covered by exactly one of
	// SandboxActive or SandboxDead. If a new status is added to the
	// schema without updating these helpers, this test will fail.
	allStatuses := []compute_v1alpha.SandboxStatus{
		compute_v1alpha.PENDING,
		compute_v1alpha.NOT_READY,
		compute_v1alpha.RUNNING,
		compute_v1alpha.STOPPED,
		compute_v1alpha.DEAD,
	}

	for _, s := range allStatuses {
		active := SandboxActive(s)
		dead := SandboxDead(s)

		if !active && !dead {
			t.Errorf("status %q is neither Active nor Dead", s)
		}
		if active && dead {
			t.Errorf("status %q is both Active and Dead", s)
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
