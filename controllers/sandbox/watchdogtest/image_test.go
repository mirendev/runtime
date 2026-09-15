package watchdogtest

import (
	"context"
	"log/slog"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/testutils"
)

func TestImageWatchdog(t *testing.T) {
	// Share test infrastructure across subtests for speed
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	cc := testDeps.CC
	eac := testDeps.EAC
	ii := testDeps.NewImageImporter()

	// Set up context with namespace
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, ii.Namespace)

	// Pull images once - they'll be reused across subtests
	_, err := cc.Pull(ctx, imagerefs.AlpineDefault, containerd.WithPullUnpack)
	require.NoError(t, err)
	_, err = cc.Pull(ctx, imagerefs.Pause, containerd.WithPullUnpack)
	require.NoError(t, err)
	_, err = cc.Pull(ctx, imagerefs.BusyboxDefault, containerd.WithPullUnpack)
	require.NoError(t, err)

	t.Run("deletes images with archived artifacts", func(t *testing.T) {
		r := require.New(t)

		// Create an App entity
		appID := entity.Id("app/" + idgen.GenNS("app"))
		app := &core_v1alpha.App{ID: appID}
		var appRpcE entityserver_v1alpha.Entity
		appRpcE.SetId(appID.String())
		appRpcE.SetAttrs(entity.New(entity.DBId, appID, app.Encode).Attrs())
		_, err := eac.Put(ctx, &appRpcE)
		r.NoError(err)

		// Create an archived artifact that maps to an image
		archivedArtifactName := idgen.GenNS("a")
		archivedImage := "cluster.local:5000/test-app:" + archivedArtifactName
		// Tag the test image with our artifact name
		img, err := cc.GetImage(ctx, imagerefs.AlpineDefault)
		r.NoError(err)
		_, err = cc.ImageService().Create(ctx, images.Image{
			Name:   archivedImage,
			Target: img.Target(),
		})
		r.NoError(err)

		// Create the archived artifact entity
		archivedArtifactID := entity.Id("artifact/" + archivedArtifactName)
		archivedArtifact := &core_v1alpha.Artifact{
			ID:     archivedArtifactID,
			App:    appID,
			Status: core_v1alpha.ARCHIVED,
		}
		var artRpcE entityserver_v1alpha.Entity
		artRpcE.SetId(archivedArtifactID.String())
		artRpcE.SetAttrs(entity.New(entity.DBId, archivedArtifactID, archivedArtifact.Encode).Attrs())
		_, err = eac.Put(ctx, &artRpcE)
		r.NoError(err)

		// Verify image exists
		_, err = cc.GetImage(ctx, archivedImage)
		r.NoError(err, "archived image should exist before GC")

		// Create the image watchdog
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    sandbox.DefaultImageGCConfig(),
		}

		// Run GC
		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		// Verify the archived image was deleted
		r.Contains(result.DeletedImages, archivedImage, "archived image should be deleted")
	})

	t.Run("keeps images with active artifacts", func(t *testing.T) {
		r := require.New(t)

		// Create an App entity
		appID := entity.Id("app/" + idgen.GenNS("app"))
		app := &core_v1alpha.App{ID: appID}
		var appRpcE entityserver_v1alpha.Entity
		appRpcE.SetId(appID.String())
		appRpcE.SetAttrs(entity.New(entity.DBId, appID, app.Encode).Attrs())
		_, err := eac.Put(ctx, &appRpcE)
		r.NoError(err)

		// Create an image with artifact naming and active artifact
		activeArtifactName := idgen.GenNS("a")
		activeImage := "cluster.local:5000/test-app-active:" + activeArtifactName
		img, err := cc.GetImage(ctx, imagerefs.AlpineDefault)
		r.NoError(err)
		_, err = cc.ImageService().Create(ctx, images.Image{
			Name:   activeImage,
			Target: img.Target(),
		})
		r.NoError(err)

		// Create the active artifact entity
		activeArtifactID := entity.Id("artifact/" + activeArtifactName)
		activeArtifact := &core_v1alpha.Artifact{
			ID:     activeArtifactID,
			App:    appID,
			Status: core_v1alpha.ACTIVE,
		}
		var artRpcE entityserver_v1alpha.Entity
		artRpcE.SetId(activeArtifactID.String())
		artRpcE.SetAttrs(entity.New(entity.DBId, activeArtifactID, activeArtifact.Encode).Attrs())
		_, err = eac.Put(ctx, &artRpcE)
		r.NoError(err)

		// Create the image watchdog
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    sandbox.DefaultImageGCConfig(),
		}

		// Run GC
		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		// Verify the active image was NOT deleted
		r.NotContains(result.DeletedImages, activeImage, "active image should not be deleted")

		// Verify image still exists
		_, err = cc.GetImage(ctx, activeImage)
		r.NoError(err, "active image should still exist after GC")
	})

	t.Run("keeps images with no artifact (infrastructure)", func(t *testing.T) {
		r := require.New(t)

		// Create the image watchdog
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    sandbox.DefaultImageGCConfig(),
		}

		// Run GC
		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		// Verify the infrastructure image was NOT deleted
		r.NotContains(result.DeletedImages, imagerefs.Pause, "infrastructure image should not be deleted")

		// Verify image still exists
		_, err = cc.GetImage(ctx, imagerefs.Pause)
		r.NoError(err, "infrastructure image should still exist after GC")
	})

	t.Run("keeps images with empty status artifact (backwards compat)", func(t *testing.T) {
		r := require.New(t)

		// Create an App entity
		appID := entity.Id("app/" + idgen.GenNS("app"))
		app := &core_v1alpha.App{ID: appID}
		var appRpcE entityserver_v1alpha.Entity
		appRpcE.SetId(appID.String())
		appRpcE.SetAttrs(entity.New(entity.DBId, appID, app.Encode).Attrs())
		_, err := eac.Put(ctx, &appRpcE)
		r.NoError(err)

		// Create an image with artifact naming but artifact has empty status (legacy)
		legacyArtifactName := idgen.GenNS("a")
		legacyImage := "cluster.local:5000/test-app-legacy:" + legacyArtifactName
		img, err := cc.GetImage(ctx, imagerefs.BusyboxDefault)
		r.NoError(err)
		_, err = cc.ImageService().Create(ctx, images.Image{
			Name:   legacyImage,
			Target: img.Target(),
		})
		r.NoError(err)

		// Create the artifact entity with NO status (legacy artifact)
		legacyArtifactID := entity.Id("artifact/" + legacyArtifactName)
		legacyArtifact := &core_v1alpha.Artifact{
			ID:     legacyArtifactID,
			App:    appID,
			Status: "", // Empty status - backwards compatibility
		}
		var artRpcE entityserver_v1alpha.Entity
		artRpcE.SetId(legacyArtifactID.String())
		artRpcE.SetAttrs(entity.New(entity.DBId, legacyArtifactID, legacyArtifact.Encode).Attrs())
		_, err = eac.Put(ctx, &artRpcE)
		r.NoError(err)

		// Create the image watchdog
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    sandbox.DefaultImageGCConfig(),
		}

		// Run GC
		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		// Verify the legacy image was NOT deleted
		r.NotContains(result.DeletedImages, legacyImage, "legacy image should not be deleted")

		// Verify image still exists
		_, err = cc.GetImage(ctx, legacyImage)
		r.NoError(err, "legacy image should still exist after GC")
	})

	t.Run("keeps images in use by running sandboxes", func(t *testing.T) {
		r := require.New(t)

		// Create an App entity
		appID := entity.Id("app/" + idgen.GenNS("app"))
		app := &core_v1alpha.App{ID: appID}
		var appRpcE entityserver_v1alpha.Entity
		appRpcE.SetId(appID.String())
		appRpcE.SetAttrs(entity.New(entity.DBId, appID, app.Encode).Attrs())
		_, err := eac.Put(ctx, &appRpcE)
		r.NoError(err)

		// Create an image with artifact naming and ARCHIVED artifact
		inUseArtifactName := idgen.GenNS("a")
		inUseImage := "cluster.local:5000/test-app-inuse:" + inUseArtifactName
		img, err := cc.GetImage(ctx, imagerefs.AlpineDefault)
		r.NoError(err)
		_, err = cc.ImageService().Create(ctx, images.Image{
			Name:   inUseImage,
			Target: img.Target(),
		})
		r.NoError(err)

		// Create the ARCHIVED artifact entity (would normally be deleted)
		inUseArtifactID := entity.Id("artifact/" + inUseArtifactName)
		inUseArtifact := &core_v1alpha.Artifact{
			ID:     inUseArtifactID,
			App:    appID,
			Status: core_v1alpha.ARCHIVED, // Archived but still in use!
		}
		var artRpcE entityserver_v1alpha.Entity
		artRpcE.SetId(inUseArtifactID.String())
		artRpcE.SetAttrs(entity.New(entity.DBId, inUseArtifactID, inUseArtifact.Encode).Attrs())
		_, err = eac.Put(ctx, &artRpcE)
		r.NoError(err)

		// Create a RUNNING sandbox that references this image
		sbID := entity.Id(idgen.GenNS("sb"))
		sb := &compute.Sandbox{
			ID:     sbID,
			Status: compute.RUNNING,
			Spec: compute.SandboxSpec{
				Container: []compute.SandboxSpecContainer{
					{
						Name:  "main",
						Image: inUseImage,
					},
				},
			},
		}
		var sbRpcE entityserver_v1alpha.Entity
		sbRpcE.SetId(sbID.String())
		sbRpcE.SetAttrs(entity.New(entity.DBId, sbID, sb.Encode).Attrs())
		_, err = eac.Put(ctx, &sbRpcE)
		r.NoError(err)

		// Create the image watchdog
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    sandbox.DefaultImageGCConfig(),
		}

		// Run GC
		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		// Verify the in-use image was NOT deleted even though artifact is archived
		r.NotContains(result.DeletedImages, inUseImage, "in-use image should not be deleted")

		// Verify image still exists
		_, err = cc.GetImage(ctx, inUseImage)
		r.NoError(err, "in-use image should still exist after GC")
	})

	t.Run("deletes orphaned images once past the grace period", func(t *testing.T) {
		r := require.New(t)

		// A miren-managed image whose artifact entity no longer exists, as
		// left behind when an app is deleted.
		orphanImage := "cluster.local:5000/test-app-deleted:" + idgen.GenNS("a")
		img, err := cc.GetImage(ctx, imagerefs.AlpineDefault)
		r.NoError(err)
		_, err = cc.ImageService().Create(ctx, images.Image{
			Name:   orphanImage,
			Target: img.Target(),
		})
		r.NoError(err)

		// containerd stamps CreatedAt itself, so age the image past a short
		// grace period rather than trying to back-date it.
		cfg := sandbox.DefaultImageGCConfig()
		cfg.OrphanGracePeriod = 100 * time.Millisecond
		time.Sleep(2 * cfg.OrphanGracePeriod)
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    cfg,
		}

		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		r.Contains(result.DeletedImages, orphanImage, "orphaned image should be deleted")
		_, err = cc.GetImage(ctx, orphanImage)
		r.Error(err, "orphaned image should not exist after GC")
	})

	t.Run("keeps orphaned images inside the grace period", func(t *testing.T) {
		r := require.New(t)

		orphanImage := "cluster.local:5000/test-app-fresh:" + idgen.GenNS("a")
		img, err := cc.GetImage(ctx, imagerefs.AlpineDefault)
		r.NoError(err)
		_, err = cc.ImageService().Create(ctx, images.Image{
			Name:   orphanImage,
			Target: img.Target(),
		})
		r.NoError(err)

		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    sandbox.DefaultImageGCConfig(),
		}

		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		r.NotContains(result.DeletedImages, orphanImage, "fresh orphaned image should not be deleted")
		_, err = cc.GetImage(ctx, orphanImage)
		r.NoError(err, "fresh orphaned image should still exist after GC")
	})

	t.Run("keeps images a surviving app version still references", func(t *testing.T) {
		r := require.New(t)

		appID := entity.Id("app/" + idgen.GenNS("app"))
		app := &core_v1alpha.App{ID: appID}
		var appRpcE entityserver_v1alpha.Entity
		appRpcE.SetId(appID.String())
		appRpcE.SetAttrs(entity.New(entity.DBId, appID, app.Encode).Attrs())
		_, err := eac.Put(ctx, &appRpcE)
		r.NoError(err)

		img, err := cc.GetImage(ctx, imagerefs.AlpineDefault)
		r.NoError(err)

		// An image whose artifact is gone, as an install upgraded from before
		// app delete spared artifacts can have. The version points at it by
		// artifact ID, so the artifact has to exist when the version is
		// created and be deleted out from under it afterwards.
		orphanArtifactName := idgen.GenNS("a")
		orphanImage := "cluster.local:5000/test-app-shared:" + orphanArtifactName
		_, err = cc.ImageService().Create(ctx, images.Image{Name: orphanImage, Target: img.Target()})
		r.NoError(err)
		orphanArtifactID := entity.Id("artifact/" + orphanArtifactName)
		orphanArtifact := &core_v1alpha.Artifact{ID: orphanArtifactID, App: appID, Status: core_v1alpha.ACTIVE}
		var orphanArtRpcE entityserver_v1alpha.Entity
		orphanArtRpcE.SetId(orphanArtifactID.String())
		orphanArtRpcE.SetAttrs(entity.New(entity.DBId, orphanArtifactID, orphanArtifact.Encode).Attrs())
		_, err = eac.Put(ctx, &orphanArtRpcE)
		r.NoError(err)

		// An image whose artifact was archived under a live version, as a
		// build reusing a digest can race the artifact GC into. The version
		// points at it by image URL only.
		archivedArtifactName := idgen.GenNS("a")
		archivedImage := "cluster.local:5000/test-app-raced:" + archivedArtifactName
		_, err = cc.ImageService().Create(ctx, images.Image{Name: archivedImage, Target: img.Target()})
		r.NoError(err)
		archivedArtifactID := entity.Id("artifact/" + archivedArtifactName)
		archivedArtifact := &core_v1alpha.Artifact{ID: archivedArtifactID, App: appID, Status: core_v1alpha.ARCHIVED}
		var artRpcE entityserver_v1alpha.Entity
		artRpcE.SetId(archivedArtifactID.String())
		artRpcE.SetAttrs(entity.New(entity.DBId, archivedArtifactID, archivedArtifact.Encode).Attrs())
		_, err = eac.Put(ctx, &artRpcE)
		r.NoError(err)

		for _, av := range []*core_v1alpha.AppVersion{
			{ID: entity.Id("app_version/" + idgen.GenNS("v")), App: appID, Artifact: orphanArtifactID},
			{ID: entity.Id("app_version/" + idgen.GenNS("v")), App: appID, ImageUrl: archivedImage},
		} {
			var avRpcE entityserver_v1alpha.Entity
			avRpcE.SetId(av.ID.String())
			avRpcE.SetAttrs(entity.New(entity.DBId, av.ID, av.Encode).Attrs())
			_, err = eac.Put(ctx, &avRpcE)
			r.NoError(err)
		}

		_, err = eac.Delete(ctx, orphanArtifactID.String())
		r.NoError(err)

		cfg := sandbox.DefaultImageGCConfig()
		cfg.OrphanGracePeriod = 100 * time.Millisecond
		time.Sleep(2 * cfg.OrphanGracePeriod)
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config:    cfg,
		}

		result, err := watchdog.RunGC(ctx)
		r.NoError(err)

		r.NotContains(result.DeletedImages, orphanImage, "image referenced by a version's artifact should not be deleted")
		r.NotContains(result.DeletedImages, archivedImage, "image referenced by a version's image URL should not be deleted")
		_, err = cc.GetImage(ctx, orphanImage)
		r.NoError(err)
		_, err = cc.GetImage(ctx, archivedImage)
		r.NoError(err)
	})

	t.Run("ParseArtifactID extracts artifact ID correctly", func(t *testing.T) {
		r := require.New(t)

		watchdog := &sandbox.ImageWatchdog{}

		// Valid image names
		r.Equal("artifact/a-abc123", watchdog.ParseArtifactID("cluster.local:5000/myapp:a-abc123"))
		r.Equal("artifact/a-xyz789", watchdog.ParseArtifactID("cluster.local:5000/another-app:a-xyz789"))

		// Invalid image names
		r.Equal("", watchdog.ParseArtifactID("docker.io/library/alpine:latest"))
		r.Equal("", watchdog.ParseArtifactID("oci.miren.cloud/pause:v1"))
		r.Equal("", watchdog.ParseArtifactID("cluster.local:5000/myapp")) // No tag
		r.Equal("", watchdog.ParseArtifactID("invalid"))
	})

	t.Run("starts and stops gracefully", func(t *testing.T) {
		// Create the watchdog with very short intervals
		watchdog := &sandbox.ImageWatchdog{
			Log:       slog.Default(),
			CC:        cc,
			EAC:       eac,
			Namespace: ii.Namespace,
			DataPath:  "/tmp",
			Config: sandbox.ImageGCConfig{
				ScheduledGCInterval:   100 * time.Millisecond,
				PressureCheckInterval: 50 * time.Millisecond,
				DiskPressureThreshold: 99.0, // High threshold so it won't trigger
			},
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
