package compute

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
)

type sandboxReadRace struct {
	entityserver_v1alpha.EntityAccess
	afterRead func() error
}

func (s *sandboxReadRace) Get(ctx context.Context, call *entityserver_v1alpha.EntityAccessGet) error {
	if err := s.EntityAccess.Get(ctx, call); err != nil {
		return err
	}
	if s.afterRead != nil {
		f := s.afterRead
		s.afterRead = nil
		return f()
	}
	return nil
}

func TestStopSandboxPreservesConcurrentRunnerUpdates(t *testing.T) {
	for _, race := range []bool{false, true} {
		name := "stop"
		if race {
			name = "runner marks dead after read"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			sandbox := &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING}
			sandbox.Spec.Version = "app_version/original"
			id, err := inmem.Client.Create(ctx, "shop", sandbox)
			require.NoError(t, err)
			proxy := &sandboxReadRace{EntityAccess: inmem.Server}
			if race {
				proxy.afterRead = func() error {
					sandbox.ID = id
					sandbox.Status = compute_v1alpha.DEAD
					sandbox.Spec.Version = "app_version/runner-update"
					return inmem.Client.Update(ctx, sandbox)
				}
			}
			client := NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(proxy)))
			err = client.StopSandbox(ctx, id.String())
			if race {
				require.ErrorIs(t, err, cond.ErrConflict{})
			} else {
				require.NoError(t, err)
			}
			var stored compute_v1alpha.Sandbox
			require.NoError(t, inmem.Client.GetById(ctx, id, &stored))
			if race {
				require.Equal(t, compute_v1alpha.DEAD, stored.Status)
				require.Equal(t, "app_version/runner-update", stored.Spec.Version.String())
			} else {
				require.Equal(t, compute_v1alpha.STOPPED, stored.Status)
				require.Equal(t, "app_version/original", stored.Spec.Version.String())
			}
		})
	}
}

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
