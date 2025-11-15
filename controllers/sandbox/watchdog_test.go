package sandbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	"log/slog"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/image"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/testutils"
)

func TestContainerWatchdog(t *testing.T) {
	t.Run("removes orphaned containers", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&cc, &eac)
		r.NoError(err)

		var ii image.ImageImporter
		err = reg.Populate(&ii)
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		// Create a sandbox entity in the store
		sbID := entity.Id(idgen.GenNS("sb"))
		sb := &compute.Sandbox{
			ID:     sbID,
			Status: compute.RUNNING,
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sbID.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, sbID,
			sb.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		// Create a valid container (one that should be kept)
		pauseID := pauseContainerId(sbID)
		validContainer, err := createTestContainer(ctx, cc, pauseID, map[string]string{
			sandboxEntityLabel: sbID.String(),
		})
		r.NoError(err)
		defer testutils.ClearContainer(ctx, validContainer)

		// Create an orphaned container (one that should be removed)
		orphanedSbID := entity.Id(idgen.GenNS("sb"))
		orphanedID := pauseContainerId(orphanedSbID)
		_, err = createTestContainer(ctx, cc, orphanedID, map[string]string{
			sandboxEntityLabel: orphanedSbID.String(),
		})
		r.NoError(err)

		// Verify orphaned container exists
		_, err = cc.LoadContainer(ctx, orphanedID)
		r.NoError(err, "orphaned container should exist before watchdog runs")

		// Create and start the watchdog with a short check interval
		watchdog := &ContainerWatchdog{
			Log:           slog.Default(),
			CC:            cc,
			EAC:           eac,
			Namespace:     ii.Namespace,
			CheckInterval: 100 * time.Millisecond,
		}

		// Run cleanup once
		result, err := watchdog.cleanupOrphanedContainers(ctx)
		r.NoError(err)

		// Verify the result contains the orphaned container
		r.Len(result.DeletedContainers, 1, "should have deleted 1 orphaned container")
		r.Contains(result.DeletedContainers, orphanedID, "deleted list should contain orphaned container")
		r.Empty(result.FailedContainers, "should have no failed deletions")

		// Verify orphaned container was removed
		_, err = cc.LoadContainer(ctx, orphanedID)
		r.Error(err, "orphaned container should be removed")

		// Verify valid container still exists
		_, err = cc.LoadContainer(ctx, pauseID)
		r.NoError(err, "valid container should still exist")
	})

	t.Run("skips non-sandbox containers", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&cc, &eac)
		r.NoError(err)

		var ii image.ImageImporter
		err = reg.Populate(&ii)
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		// Create a non-sandbox container (no sandbox labels)
		nonSandboxID := "non-sandbox-container"
		nonSandboxContainer, err := createTestContainer(ctx, cc, nonSandboxID, map[string]string{
			"some-other-label": "value",
		})
		r.NoError(err)
		defer testutils.ClearContainer(ctx, nonSandboxContainer)

		// Create the watchdog
		watchdog := &ContainerWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
		}

		// Run cleanup
		result, err := watchdog.cleanupOrphanedContainers(ctx)
		r.NoError(err)

		// Verify result shows no containers were deleted
		r.Empty(result.DeletedContainers, "should have deleted 0 containers")
		r.Empty(result.FailedContainers, "should have no failed deletions")

		// Verify non-sandbox container was NOT removed
		_, err = cc.LoadContainer(ctx, nonSandboxID)
		r.NoError(err, "non-sandbox container should not be removed")
	})

	t.Run("removes containers from old DEAD sandboxes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&cc, &eac)
		r.NoError(err)

		var ii image.ImageImporter
		err = reg.Populate(&ii)
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		// Create a sandbox entity marked as DEAD
		oldDeadSbID := entity.Id(idgen.GenNS("sb"))
		oldDeadSb := &compute.Sandbox{
			ID:     oldDeadSbID,
			Status: compute.DEAD,
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(oldDeadSbID.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, oldDeadSbID,
			oldDeadSb.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		// Wait for the sandbox to be "old" (> grace window)
		// Using a very short grace window for testing
		time.Sleep(100 * time.Millisecond)

		// Create a container for this old DEAD sandbox
		oldDeadContainerID := pauseContainerId(oldDeadSbID)
		oldDeadContainer, err := createTestContainer(ctx, cc, oldDeadContainerID, map[string]string{
			sandboxEntityLabel: oldDeadSbID.String(),
		})
		r.NoError(err)
		defer testutils.ClearContainer(ctx, oldDeadContainer)

		// Verify old DEAD container exists
		_, err = cc.LoadContainer(ctx, oldDeadContainerID)
		r.NoError(err, "old DEAD sandbox container should exist before watchdog runs")

		// Create the watchdog
		watchdog := &ContainerWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
		}

		// Create the watchdog with a very short grace window
		watchdog.GraceWindow = 10 * time.Millisecond

		// Run cleanup
		result, err := watchdog.cleanupOrphanedContainers(ctx)
		r.NoError(err)

		// Verify the old DEAD container was removed
		r.Len(result.DeletedContainers, 1, "should have deleted 1 old DEAD container")
		r.Contains(result.DeletedContainers, oldDeadContainerID, "deleted list should contain old DEAD container")
		r.Empty(result.FailedContainers, "should have no failed deletions")

		// Verify container was actually removed
		_, err = cc.LoadContainer(ctx, oldDeadContainerID)
		r.Error(err, "old DEAD sandbox container should be removed")
	})

	t.Run("keeps containers from recently updated DEAD sandboxes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&cc, &eac)
		r.NoError(err)

		var ii image.ImageImporter
		err = reg.Populate(&ii)
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		// Create a sandbox entity marked as DEAD (recently)
		recentDeadSbID := entity.Id(idgen.GenNS("sb"))
		recentDeadSb := &compute.Sandbox{
			ID:     recentDeadSbID,
			Status: compute.DEAD,
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(recentDeadSbID.String())
		rpcE.SetAttrs(entity.New(
			entity.DBId, recentDeadSbID,
			recentDeadSb.Encode).Attrs())
		_, err = eac.Put(ctx, &rpcE)
		r.NoError(err)

		// DON'T wait - run cleanup immediately so it's within grace window

		// Create a container for this recently DEAD sandbox
		recentDeadContainerID := pauseContainerId(recentDeadSbID)
		recentDeadContainer, err := createTestContainer(ctx, cc, recentDeadContainerID, map[string]string{
			sandboxEntityLabel: recentDeadSbID.String(),
		})
		r.NoError(err)
		defer testutils.ClearContainer(ctx, recentDeadContainer)

		// Verify recently DEAD container exists
		_, err = cc.LoadContainer(ctx, recentDeadContainerID)
		r.NoError(err, "recently DEAD sandbox container should exist before watchdog runs")

		// Create the watchdog with a longer grace window
		watchdog := &ContainerWatchdog{
			Log:         slog.Default(),
			CC:          cc,
			EAC:         eac,
			Namespace:   ii.Namespace,
			GraceWindow: 10 * time.Second,
		}

		// Run cleanup
		result, err := watchdog.cleanupOrphanedContainers(ctx)
		r.NoError(err)

		// Verify the recently DEAD container was NOT removed (grace period)
		r.Empty(result.DeletedContainers, "should have deleted 0 containers")
		r.Empty(result.FailedContainers, "should have no failed deletions")

		// Verify container still exists
		_, err = cc.LoadContainer(ctx, recentDeadContainerID)
		r.NoError(err, "recently DEAD sandbox container should NOT be removed due to grace period")
	})

	t.Run("starts and stops gracefully", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			eac *entityserver_v1alpha.EntityAccessClient
		)

		err := reg.Init(&cc, &eac)
		r.NoError(err)

		var ii image.ImageImporter
		err = reg.Populate(&ii)
		r.NoError(err)

		// Create the watchdog with a very short interval
		watchdog := &ContainerWatchdog{
			Log:           slog.Default(),
			CC:            cc,
			EAC:           eac,
			Namespace:     ii.Namespace,
			CheckInterval: 100 * time.Millisecond,
		}

		// Start the watchdog
		watchdog.Start(ctx)

		// Let it run for a bit
		time.Sleep(300 * time.Millisecond)

		// Stop the watchdog
		watchdog.Stop()

		// Should complete without hanging
		time.Sleep(200 * time.Millisecond)
	})
}

// createTestContainer creates a test container for the watchdog tests
func createTestContainer(ctx context.Context, cc *containerd.Client, id string, labels map[string]string) (containerd.Container, error) {
	// Pull busybox image if needed
	img, err := cc.GetImage(ctx, "docker.io/library/busybox:latest")
	if err != nil {
		img, err = cc.Pull(ctx, "docker.io/library/busybox:latest", containerd.WithPullUnpack)
		if err != nil {
			return nil, fmt.Errorf("failed to pull busybox image: %w", err)
		}
	}

	container, err := cc.NewContainer(ctx,
		id,
		containerd.WithNewSnapshot(id+"-snapshot", img),
		containerd.WithNewSpec(
			oci.WithImageConfig(img),
			oci.WithProcessArgs("/bin/sh", "-c", "sleep 1000"),
		),
		containerd.WithRuntime("io.containerd.runc.v2", nil),
		containerd.WithAdditionalContainerLabels(labels),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	return container, nil
}
