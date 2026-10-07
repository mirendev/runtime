package sandbox

import (
	"context"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/testutils"
)

// These scripts loop on a backgrounded sleep so the shell's wait is
// interruptible and a trapped SIGTERM runs as soon as it arrives.
const (
	exitOnTerm   = `trap 'exit 0' TERM; while true; do sleep 1 & wait; done`
	ignoreOnTerm = `trap 'echo draining' TERM; while true; do sleep 1 & wait; done`
)

// TestDestroySubContainersAcrossRestart covers a drain that outlives the server
// process driving it (MIR-1994): the old server must leave it running, and the
// next one must resume it on the original deadline without signalling again.
func TestDestroySubContainersAcrossRestart(t *testing.T) {
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	ctx := namespaces.WithNamespace(context.Background(), testDeps.Namespace)

	setupCtx, setupCancel := context.WithTimeout(ctx, 120*time.Second)
	defer setupCancel()
	img, err := testDeps.CC.Pull(setupCtx, "docker.io/library/busybox:latest", containerd.WithPullUnpack)
	require.NoError(t, err)

	co, err := newSandboxController(testDeps)
	require.NoError(t, err)
	defer co.Close()

	// startSubContainer runs script as the "app" subcontainer of a fresh
	// sandbox id, labelled the way buildSubContainerOpts would.
	startSubContainer := func(t *testing.T, script string, labels map[string]string) (entity.Id, containerd.Container, containerd.Task) {
		r := require.New(t)
		id := entity.Id(idgen.GenNS("sb"))
		containerID := containerPrefix(id) + "-app"

		cont, err := testDeps.CC.NewContainer(ctx, containerID,
			containerd.WithNewSnapshot(containerID, img),
			containerd.WithRuntime("io.containerd.runc.v2", nil),
			containerd.WithNewSpec(
				oci.WithDefaultSpec(),
				oci.WithImageConfig(img),
				oci.WithProcessArgs("/bin/sh", "-c", script),
			),
			containerd.WithAdditionalContainerLabels(labels),
		)
		r.NoError(err)
		t.Cleanup(func() { testutils.ClearContainer(context.WithoutCancel(ctx), cont) })

		task, err := cont.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, nil, nil)))
		r.NoError(err)
		r.NoError(task.Start(ctx))

		// Let the shell install its trap before anything signals it.
		time.Sleep(300 * time.Millisecond)

		return id, cont, task
	}

	requireGone := func(t *testing.T, cont containerd.Container) {
		_, err := testDeps.CC.LoadContainer(ctx, cont.ID())
		require.True(t, errdefs.IsNotFound(err), "container should be deleted, got %v", err)
	}

	t.Run("fresh drain records the stop and exits on SIGTERM", func(t *testing.T) {
		r := require.New(t)
		id, cont, _ := startSubContainer(t, exitOnTerm, map[string]string{
			shutdownTimeoutLabel: "30s",
		})

		start := time.Now()
		r.NoError(co.DestroySubContainers(ctx, id))
		r.Less(time.Since(start), 5*time.Second)
		requireGone(t, cont)
	})

	t.Run("server shutdown hands the drain off without killing it", func(t *testing.T) {
		r := require.New(t)
		id, cont, task := startSubContainer(t, ignoreOnTerm, map[string]string{
			shutdownTimeoutLabel: "30s",
		})

		serverCtx, serverStop := context.WithCancel(ctx)
		time.AfterFunc(500*time.Millisecond, serverStop)

		err := co.DestroySubContainers(serverCtx, id)
		r.ErrorIs(err, errDrainHandedOff)

		status, err := task.Status(ctx)
		r.NoError(err)
		r.Equal(containerd.Running, status.Status, "a handed-off drain must keep running")

		labels, err := cont.Labels(ctx)
		r.NoError(err)
		stamp, err := time.Parse(time.RFC3339Nano, labels[stopRequestedLabel])
		r.NoError(err, "stop request should be recorded on the container")
		r.WithinDuration(time.Now(), stamp, 5*time.Second)
	})

	t.Run("shutdown landing mid-signal still signals and hands off", func(t *testing.T) {
		r := require.New(t)
		id, cont, task := startSubContainer(t, exitOnTerm, map[string]string{
			shutdownTimeoutLabel: "30s",
		})
		exitCh, err := task.Wait(ctx)
		r.NoError(err)

		// Cancel between attaching to the task and signalling it. That window
		// could otherwise stamp a stop that was never sent, or fall through to
		// cleanup and kill a task that was never signalled.
		serverCtx, serverStop := context.WithCancel(ctx)
		co.onTaskAttached = func(string) { serverStop() }
		defer func() { co.onTaskAttached = nil }()

		_, err = co.destroySubContainers(serverCtx, id, nil)
		r.ErrorIs(err, errDrainHandedOff)

		labels, err := cont.Labels(ctx)
		r.NoError(err)
		r.NotEmpty(labels[stopRequestedLabel], "the stop should be recorded alongside its SIGTERM")

		// The app exits 0 on SIGTERM; a kill from cleanup would show as 137.
		select {
		case status := <-exitCh:
			r.Equal(uint32(0), status.ExitCode(), "task should have exited on SIGTERM, not been killed")
		case <-time.After(5 * time.Second):
			r.Fail("task never received SIGTERM")
		}
	})

	t.Run("resumed drain keeps its deadline and is not signalled again", func(t *testing.T) {
		r := require.New(t)

		// The previous server sent SIGTERM 2s into a 3s window. The app exits
		// on SIGTERM, so a second one would end it at once, and a reset window
		// would run the full 3s. Only the carried deadline lands in between.
		id, cont, _ := startSubContainer(t, exitOnTerm, map[string]string{
			shutdownTimeoutLabel: "3s",
		})

		// Stamp just before resuming, so container startup doesn't eat into
		// the window being measured.
		start := time.Now()
		_, err := cont.SetLabels(ctx, map[string]string{
			stopRequestedLabel: start.Add(-2 * time.Second).UTC().Format(time.RFC3339Nano),
		})
		r.NoError(err)

		r.NoError(co.DestroySubContainers(ctx, id))
		elapsed := time.Since(start)

		r.Greater(elapsed, 700*time.Millisecond, "task exited early, so it was signalled again")
		r.Less(elapsed, 2500*time.Millisecond, "drain ran past its original deadline")
		requireGone(t, cont)
	})

	t.Run("resumed drain past its deadline is killed at once", func(t *testing.T) {
		r := require.New(t)
		id, cont, _ := startSubContainer(t, ignoreOnTerm, map[string]string{
			shutdownTimeoutLabel: "3s",
			stopRequestedLabel:   time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		})

		start := time.Now()
		r.NoError(co.DestroySubContainers(ctx, id))
		r.Less(time.Since(start), 2*time.Second)
		requireGone(t, cont)
	})

	t.Run("dead sandbox with an interrupted drain finishes teardown", func(t *testing.T) {
		r := require.New(t)
		id, cont, _ := startSubContainer(t, ignoreOnTerm, map[string]string{
			shutdownTimeoutLabel: "3s",
			stopRequestedLabel:   time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		})

		r.NoError(co.Create(ctx, &compute.Sandbox{ID: id, Status: compute.DEAD}, nil))
		requireGone(t, cont)
	})

	t.Run("dead sandbox without a drain is left alone", func(t *testing.T) {
		r := require.New(t)
		id, cont, task := startSubContainer(t, ignoreOnTerm, map[string]string{
			shutdownTimeoutLabel: "3s",
		})

		r.NoError(co.Create(ctx, &compute.Sandbox{ID: id, Status: compute.DEAD}, nil))

		status, err := task.Status(ctx)
		r.NoError(err)
		r.Equal(containerd.Running, status.Status)
		_, err = testDeps.CC.LoadContainer(ctx, cont.ID())
		r.NoError(err)
	})
}
