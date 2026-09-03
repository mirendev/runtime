package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	entityclient "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/snapshot"
	"miren.dev/runtime/servers/entityserver"
)

// failingStore wraps *entity.MockStore to inject the three failure modes the
// bug report describes. It also models the production etcd store's contract
// that the mock omits: a cancelled context fails Create/Patch/Delete. Without
// that, sub-path C's "stuck RESTORING disk because cleanup reuses the cancelled
// context" cannot be reproduced, and a test asserting the fix has no way to
// distinguish the fix (rollback uses context.WithoutCancel) from the bug
// (rollback uses the caller's context) — the mock would happily delete on a
// cancelled context either way.
type failingStore struct {
	*entity.MockStore

	// failCreatePrefix, when non-empty, makes CreateEntity fail for any entity
	// whose id has this prefix. Used to inject sub-path A (the disk_volume
	// Create RPC fails on a healthy context).
	failCreatePrefix string

	// failPatch makes PatchEntity fail unconditionally. Used to inject
	// sub-path B (the disk -> PROVISIONED Patch fails after the disk_volume
	// Create has already committed).
	failPatch bool
}

func (f *failingStore) CreateEntity(ctx context.Context, e *entity.Entity, opts ...entity.EntityOption) (*entity.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.failCreatePrefix != "" && strings.HasPrefix(string(e.Id()), f.failCreatePrefix) {
		return nil, fmt.Errorf("injected: CreateEntity failed for %s", e.Id())
	}
	return f.MockStore.CreateEntity(ctx, e, opts...)
}

func (f *failingStore) PatchEntity(ctx context.Context, e *entity.Entity, opts ...entity.EntityOption) (*entity.Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.failPatch {
		return nil, fmt.Errorf("injected: PatchEntity failed")
	}
	return f.MockStore.PatchEntity(ctx, e, opts...)
}

func (f *failingStore) DeleteEntity(ctx context.Context, id entity.Id) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.MockStore.DeleteEntity(ctx, id)
}

// setupTestEnv builds an in-process entity server backed by failingStore, plus
// the production entityDiskResolver, and a temp data path. A single coordinator
// node is seeded so CreateDiskAndVolume's findNodeId succeeds.
func setupTestEnv(t *testing.T, failCreatePrefix string, failPatch bool) (
	*failingStore,
	*entityDiskResolver,
	*entityserver_v1alpha.EntityAccessClient,
	string,
) {
	t.Helper()

	store := &failingStore{
		MockStore:        entity.NewMockStore(),
		failCreatePrefix: failCreatePrefix,
		failPatch:        failPatch,
	}

	// Seed a coordinator node so findNodeId returns exactly one node.
	node := entity.New(
		entity.Ref(entity.EntityKind, compute.KindNode),
		entity.DBId, entity.Id("node-test-coordinator"),
	)
	store.AddEntity(node.Id(), node)

	server := &entityserver.EntityServer{
		Log:   slog.Default(),
		Store: store,
	}
	eac := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}
	ec := entityclient.NewClient(slog.Default(), eac)
	resolver := newEntityDiskResolver(eac, ec)

	return store, resolver, eac, t.TempDir()
}

// runRestore mirrors the orchestration of DiskRestore in disk_restore.go:
// register the failure cleanup defer first, then create the temp file, write
// the image, rename it to its final path, and finalize. The defer registration
// order, the rename-before-Finalize sequencing, and the retErr != nil cleanup
// gate all match disk_restore.go so the production Cleanup closure is exercised
// exactly as the CLI exercises it. cleanupErrOut receives the error (if any)
// returned by target.Cleanup on the failure path.
func runRestore(
	ctx context.Context,
	target *snapshot.RestoreTarget,
	data []byte,
	cleanupErrOut *error,
) (retErr error) {
	// disk_restore.go registers the disk-entity cleanup defer (line 68) before
	// the temp file is created (line 99), so defers fire LIFO with the temp-file
	// defer first and the cleanup defer last — matching the production order.
	if target.Created && target.Cleanup != nil {
		defer func() {
			if retErr != nil {
				*cleanupErrOut = target.Cleanup(ctx)
			}
		}()
	}

	if err := os.MkdirAll(filepath.Dir(target.ImagePath), 0o755); err != nil {
		return fmt.Errorf("creating image directory: %w", err)
	}

	tmpPath := target.ImagePath + ".restore.tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp image file: %w", err)
	}
	defer func() {
		outFile.Close()
		if retErr != nil {
			// A no-op once os.Rename committed the image to ImagePath — this is
			// the dangling temp-file cleanup the bug report calls out.
			os.Remove(tmpPath)
		}
	}()

	if err := outFile.Truncate(int64(len(data))); err != nil {
		return fmt.Errorf("truncating image file: %w", err)
	}

	if _, err := outFile.Write(data); err != nil {
		return fmt.Errorf("writing restored image: %w", err)
	}

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("closing restored image: %w", err)
	}

	// Commits the image to its final path before Finalize runs — the bug's
	// root cause: once renamed, the temp-file defer above cannot retract it.
	if err := os.Rename(tmpPath, target.ImagePath); err != nil {
		return fmt.Errorf("moving restored image into place: %w", err)
	}

	if target.Finalize != nil {
		if err := target.Finalize(ctx); err != nil {
			return fmt.Errorf("finalizing restore: %w", err)
		}
	}

	return nil
}

// imageExists reports whether a regular file is present at path. Used to assert
// that the restored image was either removed by rollback or leaked.
func imageExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir()
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	t.Fatalf("unexpected error stating %s: %v", path, err)
	return false
}

// countByKind counts entities in the mock store with the given entity/kind.
func countByKind(t *testing.T, store *failingStore, kind entity.Id) int {
	t.Helper()
	n := 0
	for id, e := range store.Entities {
		// Skip the seeded node and any schema-ish entities; only count the
		// requested kind, which uniquely identifies disk/disk_volume here.
		if entity.Is(e, kind) {
			n++
			continue
		}
		_ = id
	}
	return n
}

// diskVolumeEntity fetches the disk_volume entity by its node-local volume id,
// derived from the image path the CLI wrote into (<dataPath>/disk-data/volumes/<volId>/disk.img).
func diskVolumeEntity(t *testing.T, store *failingStore, target *snapshot.RestoreTarget) (*storage_v1alpha.DiskVolume, error) {
	t.Helper()
	volId := filepath.Base(filepath.Dir(target.ImagePath))
	ent, err := store.GetEntity(context.Background(), entity.Id("disk_volume/"+volId))
	if err != nil {
		return nil, err
	}
	var vol storage_v1alpha.DiskVolume
	vol.Decode(ent)
	return &vol, nil
}

// TestDiskRestore_HappyPath verifies that a successful create-new-disk restore
// leaves the disk PROVISIONED and a single DV_READY disk_volume, with the
// image in place and no cleanup. Guards that the fix to Cleanup did not change
// Finalize's success behavior.
func TestDiskRestore_HappyPath(t *testing.T) {
	store, resolver, _, dataPath := setupTestEnv(t, "", false)

	ctx := context.Background()
	const diskName = "test-disk"

	target, err := resolver.CreateDiskAndVolume(ctx, diskName, 1<<30, "ext4", dataPath)
	require.NoError(t, err)
	require.True(t, target.Created)
	require.NotNil(t, target.Finalize)
	require.NotNil(t, target.Cleanup)

	imageData := []byte("restored-image-contents")
	var cleanupErr error
	retErr := runRestore(ctx, target, imageData, &cleanupErr)

	require.NoError(t, retErr, "happy-path restore should succeed")
	require.NoError(t, cleanupErr, "cleanup must not run on success")
	assert.True(t, imageExists(t, target.ImagePath), "image should remain at final path on success")
	assert.Equal(t, 1, countByKind(t, store, storage_v1alpha.KindDisk), "exactly one disk entity")
	assert.Equal(t, 1, countByKind(t, store, storage_v1alpha.KindDiskVolume), "exactly one disk_volume entity")

	// The disk should be PROVISIONED with the disk_volume recorded.
	disk, err := resolver.FindDisk(ctx, diskName)
	require.NoError(t, err)
	assert.Equal(t, string(storage_v1alpha.PROVISIONED), disk.Status)

	vol, err := diskVolumeEntity(t, store, target)
	require.NoError(t, err)
	assert.Equal(t, storage_v1alpha.DV_READY, vol.ActualState)
	assert.Equal(t, target.ImagePath, vol.ImagePath)

	// FindVolume must resolve the disk_volume from the disk id (the path the
	// disk controller uses to pick the volume up on reconcile).
	volState, err := resolver.FindVolume(ctx, disk.ID)
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(filepath.Dir(target.ImagePath)), volState.VolumeID)
}

// TestDiskRestore_FinalizeCreateFailureRevertsImageAndDisk reproduces sub-path A:
// the disk_volume Create RPC fails on a healthy context. The fix's Cleanup must
// remove the renamed image and delete the disk entity, leaving the system as if
// the restore never ran. Without the fix, Cleanup deletes only the disk entity
// and the image leaks at target.ImagePath.
func TestDiskRestore_FinalizeCreateFailureRevertsImageAndDisk(t *testing.T) {
	store, resolver, _, dataPath := setupTestEnv(t, "disk_volume/", false)

	ctx := context.Background()
	const diskName = "test-disk-create-fail"

	target, err := resolver.CreateDiskAndVolume(ctx, diskName, 1<<30, "ext4", dataPath)
	require.NoError(t, err)
	require.True(t, target.Created)

	// The disk entity exists and is RESTORING before the image is written.
	assert.Equal(t, 1, countByKind(t, store, storage_v1alpha.KindDisk))

	var cleanupErr error
	retErr := runRestore(ctx, target, []byte("image-data"), &cleanupErr)

	require.Error(t, retErr, "Finalize's Create should fail")
	assert.Contains(t, retErr.Error(), "creating disk_volume entity")
	require.NoError(t, cleanupErr, "rollback must succeed for a healthy-context failure")

	// The fix: image removed, no disk_volume (Create never committed), disk
	// entity rolled back to RESTORING-gone.
	assert.False(t, imageExists(t, target.ImagePath),
		"image must be removed by cleanup (was renamed before Finalize failed)")
	assert.Equal(t, 0, countByKind(t, store, storage_v1alpha.KindDiskVolume),
		"no disk_volume should exist after a Create failure")
	assert.Equal(t, 0, countByKind(t, store, storage_v1alpha.KindDisk),
		"the RESTORING disk entity must be deleted by cleanup")

	// The temp file must not linger either.
	_, statErr := os.Stat(target.ImagePath + ".restore.tmp")
	assert.True(t, errors.Is(statErr, fs.ErrNotExist), "temp file must not linger")

	// And a same-name retry must be able to start fresh (the disk is gone).
	_, err = resolver.FindDisk(ctx, diskName)
	require.Error(t, err)
}

// TestDiskRestore_FinalizePatchFailureRevertsZombieDiskVolume reproduces
// sub-path B: the disk_volume Create succeeds, then the disk -> PROVISIONED
// Patch fails on a still-healthy context. This is the severe sub-path — without
// the fix, Cleanup deletes the disk entity but leaves a DV_READY zombie
// disk_volume whose parent disk is gone, which the disk-volume controller
// adopts and mounts in the default VM_UNIVERSAL mode. The fix's Cleanup must
// delete that zombie disk_volume and the image too.
func TestDiskRestore_FinalizePatchFailureRevertsZombieDiskVolume(t *testing.T) {
	store, resolver, _, dataPath := setupTestEnv(t, "", true)

	ctx := context.Background()
	const diskName = "test-disk-patch-fail"

	target, err := resolver.CreateDiskAndVolume(ctx, diskName, 1<<30, "ext4", dataPath)
	require.NoError(t, err)
	require.True(t, target.Created)

	var cleanupErr error
	retErr := runRestore(ctx, target, []byte("image-data"), &cleanupErr)

	require.Error(t, retErr, "Finalize's Patch should fail")
	assert.Contains(t, retErr.Error(), "updating disk to provisioned")
	require.NoError(t, cleanupErr, "rollback must succeed for a healthy-context failure")

	// The disk_volume was created by Finalize before the Patch failed. The
	// fix must delete it; without the fix it would survive as a DV_READY zombie
	// referencing the now-deleted disk.
	assert.Equal(t, 0, countByKind(t, store, storage_v1alpha.KindDiskVolume),
		"the zombie disk_volume Finalize created must be deleted by cleanup")

	volId := filepath.Base(filepath.Dir(target.ImagePath))
	_, volErr := store.GetEntity(ctx, entity.Id("disk_volume/"+volId))
	assert.True(t, errors.Is(volErr, cond.ErrNotFound{}),
		"the disk_volume entity must be gone (was a zombie before the fix)")

	assert.False(t, imageExists(t, target.ImagePath),
		"the renamed image must be removed by cleanup")
	assert.Equal(t, 0, countByKind(t, store, storage_v1alpha.KindDisk),
		"the disk entity must be deleted by cleanup")
}

// TestDiskRestore_CancelledContextRollbackStillSucceeds reproduces sub-path C:
// the operator interrupts the restore (the request context is cancelled) before
// Finalize's Create dispatches, so Create fails on the cancelled context. The
// fix's Cleanup uses context.WithoutCancel so the deferred rollback still
// deletes the disk entity and removes the image despite the caller's context
// being cancelled. Without the fix, Cleanup reuses the cancelled context, its
// Delete fails too, and the result is an orphan image plus a stuck RESTORING
// disk that blocks same-name retries.
func TestDiskRestore_CancelledContextRollbackStillSucceeds(t *testing.T) {
	store, resolver, _, dataPath := setupTestEnv(t, "", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const diskName = "test-disk-cancelled"

	// CreateDiskAndVolume must run on a healthy context — it creates the disk
	// entity and resolves the node. The operator interrupts after this, before
	// Finalize's Create dispatches (matching the report: RestoreImage/os.Rename
	// are context-unaware so the cancel only surfaces at Finalize's Create).
	target, err := resolver.CreateDiskAndVolume(ctx, diskName, 1<<30, "ext4", dataPath)
	require.NoError(t, err)
	require.True(t, target.Created)
	require.Equal(t, 1, countByKind(t, store, storage_v1alpha.KindDisk))

	cancel() // operator Ctrl-C lands here; the context is now cancelled.

	var cleanupErr error
	retErr := runRestore(ctx, target, []byte("image-data"), &cleanupErr)

	require.Error(t, retErr, "Finalize's Create should fail on the cancelled context")
	assert.True(t, errors.Is(retErr, context.Canceled),
		"the failure should be the context cancellation, got: %v", retErr)

	// This is the crux of sub-path C: cleanup must succeed despite the caller's
	// context being cancelled, because the fix rolls back with WithoutCancel.
	require.NoError(t, cleanupErr,
		"cleanup must not reuse the cancelled context for its rollback RPCs")

	assert.False(t, imageExists(t, target.ImagePath),
		"the image must be removed even when rollback runs after cancellation")
	assert.Equal(t, 0, countByKind(t, store, storage_v1alpha.KindDiskVolume),
		"no disk_volume should exist (Create never committed on a cancelled ctx)")
	assert.Equal(t, 0, countByKind(t, store, storage_v1alpha.KindDisk),
		"the RESTORING disk must be deleted by rollback even after cancellation")

	// A same-name retry can start fresh — the stuck RESTORING disk that
	// blocked retries before the fix is gone.
	_, err = resolver.FindDisk(ctx, diskName)
	require.Error(t, err)
}

// TestDiskRestore_NonCreatedRestoreDoesNotInvokeCleanup guards the restore-over-
// existing-disk path, which never enters the Cleanup/Finalize machinery: the
// bug class is gated on target.Created == true. Ensures the fix's added
// rollback steps do not run when there is nothing to roll back.
func TestDiskRestore_NonCreatedRestoreDoesNotInvokeCleanup(t *testing.T) {
	store, resolver, eac, dataPath := setupTestEnv(t, "", false)
	ctx := context.Background()

	// Simulate PrepareRestore's existing-disk branch by hand: create a disk
	// in the store with a matching disk_volume, then build a RestoreTarget with
	// Created=false and nil Cleanup/Finalize, exactly as pkg/snapshot/disk.go
	// returns for a disk that already exists.
	diskId := entity.Id("disk-existing-01")
	diskEntity := entity.New(
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk),
		entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisionedId),
		entity.String(storage_v1alpha.DiskNameId, "existing-disk"),
		entity.Int64(storage_v1alpha.DiskSizeGbId, 1),
		entity.DBId, diskId,
	)
	store.AddEntity(diskEntity.Id(), diskEntity)

	volId := "vol-existing-01"
	volEntity := entity.New(
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskVolume),
		entity.Ref(storage_v1alpha.DiskVolumeDiskIdId, diskId),
		entity.String(storage_v1alpha.DiskVolumeVolumeIdId, volId),
		entity.DBId, entity.Id("disk_volume/"+volId),
	)
	store.AddEntity(volEntity.Id(), volEntity)

	target := &snapshot.RestoreTarget{
		Name:      "existing-disk",
		ImagePath: filepath.Join(dataPath, "disk-data", "volumes", volId, "disk.img"),
		Created:   false,
		Finalize:  nil,
		Cleanup:   nil,
	}

	// Sanity: the resolver sees the existing disk and its volume.
	diskState, err := resolver.FindDisk(ctx, "existing-disk")
	require.NoError(t, err)
	_, err = resolver.FindVolume(ctx, diskState.ID)
	require.NoError(t, err)

	var cleanupErr error
	retErr := runRestore(ctx, target, []byte("image-data"), &cleanupErr)

	require.NoError(t, retErr)
	require.NoError(t, cleanupErr)

	// The existing disk + disk_volume are untouched (no cleanup ran, no
	// finalize ran). The image was written to the final path.
	assert.True(t, imageExists(t, target.ImagePath))
	assert.Equal(t, 1, countByKind(t, store, storage_v1alpha.KindDisk))
	assert.Equal(t, 1, countByKind(t, store, storage_v1alpha.KindDiskVolume))

	// eac is unused on this path; reference it so the linter does not flag the
	// setup helper's otherwise-unused return.
	_ = eac
}
