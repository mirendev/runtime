package diskresolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// faultRPC wraps an rpc.Client and fails the failAt-th (1-based) call to the
// named entity-server method, simulating the partial-Finalize RPC failure the
// bug report describes (one of Finalize's two writes succeeding and the other
// failing). All other calls pass through unchanged, so the surrounding setup
// (disk creation, node lookup, reads) still works.
type faultRPC struct {
	rpc.Client
	failMethod string
	failAt     int
	failErr    error

	mu     sync.Mutex
	counts map[string]int
}

func newFaultRPC(underlying rpc.Client, method string, at int, err error) *faultRPC {
	return &faultRPC{
		Client:     underlying,
		failMethod: method,
		failAt:     at,
		failErr:    err,
		counts:     map[string]int{},
	}
}

func (f *faultRPC) Call(ctx context.Context, method string, args, result any) error {
	if f.failMethod != "" {
		f.mu.Lock()
		f.counts[method]++
		n := f.counts[method]
		f.mu.Unlock()
		if method == f.failMethod && n == f.failAt {
			return f.failErr
		}
	}
	return f.Client.Call(ctx, method, args, result)
}

// cancelAwareRPC fails every call made with an already-cancelled context, the
// way a real transport does. The in-memory entity server ignores the context
// it is handed, so without this a test could not tell whether cleanup detaches
// from the cancellation that triggered it.
type cancelAwareRPC struct {
	rpc.Client
}

func (c cancelAwareRPC) Call(ctx context.Context, method string, args, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Client.Call(ctx, method, args, result)
}

// setupResolver builds an in-memory entity server, seeds a coordinator node so
// Resolver.FindNodeId succeeds, and returns a resolver wired through
// fault (or a plain eac when fault is nil). Reads in tests should use es.EAC,
// which is not fault-injected.
func setupResolver(t *testing.T, fault *faultRPC) (*testutils.InMemEntityServer, *Resolver) {
	t.Helper()
	ctx := t.Context()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	// Seed a single coordinator node so Resolver.FindNodeId returns one.
	_, err := es.Client.Create(ctx, "coordinator", &compute.Node{ApiAddress: ":8444"})
	require.NoError(t, err)

	underlying := es.EAC.Client
	cli := underlying
	if fault != nil {
		fault.Client = underlying
		cli = fault
	}
	eac := entityserver_v1alpha.NewEntityAccessClient(cli)
	ec := entityserver.NewClient(testutils.TestLogger(t), eac)
	return es, New(eac, ec)
}

func listTestDisks(t *testing.T, ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient) []storage_v1alpha.Disk {
	t.Helper()
	resp, err := eac.List(ctx, entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk))
	require.NoError(t, err)
	var disks []storage_v1alpha.Disk
	for _, v := range resp.Values() {
		var d storage_v1alpha.Disk
		d.Decode(v.Entity())
		disks = append(disks, d)
	}
	return disks
}

func allTestDiskVolumes(t *testing.T, ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient) []storage_v1alpha.DiskVolume {
	t.Helper()
	resp, err := eac.List(ctx, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskVolume))
	require.NoError(t, err)
	var vols []storage_v1alpha.DiskVolume
	for _, v := range resp.Values() {
		var vol storage_v1alpha.DiskVolume
		vol.Decode(v.Entity())
		vols = append(vols, vol)
	}
	return vols
}

func volumesForDisk(t *testing.T, ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, diskID entity.Id) []storage_v1alpha.DiskVolume {
	t.Helper()
	resp, err := eac.List(ctx, entity.Ref(storage_v1alpha.DiskVolumeDiskIdId, diskID))
	require.NoError(t, err)
	var vols []storage_v1alpha.DiskVolume
	for _, v := range resp.Values() {
		var vol storage_v1alpha.DiskVolume
		vol.Decode(v.Entity())
		vols = append(vols, vol)
	}
	return vols
}

func getTestDisk(t *testing.T, ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, id entity.Id) storage_v1alpha.Disk {
	t.Helper()
	resp, err := eac.Get(ctx, id.String())
	require.NoError(t, err)
	var d storage_v1alpha.Disk
	d.Decode(resp.Entity().Entity())
	return d
}

// TestCreateDiskAndVolume_FinalizeSuccess is the happy path: Finalize patches
// the disk to PROVISIONED and then creates the disk_volume, leaving the disk
// carrying its VolumeId and exactly one disk_volume referencing it.
func TestCreateDiskAndVolume_FinalizeSuccess(t *testing.T) {
	ctx := t.Context()
	es, resolver := setupResolver(t, nil)

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.True(t, target.Created)
	assert.NotEmpty(t, target.ImagePath)

	disks := listTestDisks(t, ctx, es.EAC)
	require.Len(t, disks, 1)
	diskID := disks[0].ID
	assert.Equal(t, storage_v1alpha.RESTORING, disks[0].Status)

	require.NotNil(t, target.Finalize)
	require.NoError(t, target.Finalize(ctx))

	disk := getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.PROVISIONED, disk.Status)
	assert.NotEmpty(t, disk.VolumeId)

	vols := volumesForDisk(t, ctx, es.EAC, diskID)
	require.Len(t, vols, 1, "Finalize should create exactly one disk_volume")
	vol := vols[0]
	assert.Equal(t, diskID, vol.DiskId)
	assert.Equal(t, disk.VolumeId, vol.VolumeId)
	assert.Equal(t, storage_v1alpha.DV_PRESENT, vol.DesiredState)
	assert.Equal(t, storage_v1alpha.DV_READY, vol.ActualState)
	assert.Equal(t, target.ImagePath, vol.ImagePath)

	// disk_volume.NodeId must match the coordinator node the resolver found.
	nodes, err := es.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindNode))
	require.NoError(t, err)
	require.Len(t, nodes.Values(), 1)
	assert.Equal(t, nodes.Values()[0].Entity().Id(), vol.NodeId)
}

// TestCreateDiskAndVolume_FinalizeCreateFailsLeavesNoOrphan is the core bug
// repro, inverted: with the reordered Finalize (Patch before Create), failing
// the disk_volume Create leaves NO disk_volume committed. Under the old
// Create-first order this is exactly the window that orphaned a disk_volume.
func TestCreateDiskAndVolume_FinalizeCreateFailsLeavesNoOrphan(t *testing.T) {
	ctx := t.Context()
	fault := newFaultRPC(nil, "create", 1, fmt.Errorf("simulated disk_volume create failure"))
	es, resolver := setupResolver(t, fault)

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)

	disks := listTestDisks(t, ctx, es.EAC)
	require.Len(t, disks, 1)
	diskID := disks[0].ID

	err = target.Finalize(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating disk_volume entity")

	// No disk_volume entity was committed: the Create that would have committed
	// it ran last and failed. This is the invariant the reorder restores.
	assert.Empty(t, allTestDiskVolumes(t, ctx, es.EAC), "no disk_volume must be left behind")
	assert.Empty(t, volumesForDisk(t, ctx, es.EAC, diskID))

	// The disk survived and reached PROVISIONED (the Patch ran first), so it can
	// drive self-healing / DELETING-based cleanup instead of being deleted out
	// from under a volume the coordinator may already be reconciling.
	disk := getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.PROVISIONED, disk.Status)
	assert.NotEmpty(t, disk.VolumeId)

	// Cleanup keeps the disk alive by transitioning it to DELETING, not by
	// deleting it outright; the disk controller's DELETING path then drives
	// any further tear-down.
	require.NotNil(t, target.Cleanup)
	require.NoError(t, target.Cleanup(ctx))
	disk = getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.DELETING, disk.Status)
	assert.Empty(t, allTestDiskVolumes(t, ctx, es.EAC))
}

// TestCreateDiskAndVolume_FinalizePatchFailsLeavesNoOrphan covers the other
// half of the partial-Finalize window: the Patch to PROVISIONED fails. With the
// reorder that means no disk_volume was ever created, so there is nothing to
// orphan; Cleanup transitions the still-RESTORING disk to DELETING.
func TestCreateDiskAndVolume_FinalizePatchFailsLeavesNoOrphan(t *testing.T) {
	ctx := t.Context()
	fault := newFaultRPC(nil, "patch", 1, fmt.Errorf("simulated disk patch failure"))
	es, resolver := setupResolver(t, fault)

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)

	disks := listTestDisks(t, ctx, es.EAC)
	require.Len(t, disks, 1)
	diskID := disks[0].ID

	err = target.Finalize(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "updating disk to provisioned")

	// Patch is now the first write, so a Patch failure cannot have left a
	// disk_volume behind.
	assert.Empty(t, allTestDiskVolumes(t, ctx, es.EAC))
	assert.Empty(t, volumesForDisk(t, ctx, es.EAC, diskID))

	// Disk still exists, still RESTORING (Patch failed, Create never ran).
	disk := getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.RESTORING, disk.Status)

	// Cleanup's Patch is the 2nd patch call; only the 1st (Finalize's) fails,
	// so cleanup still transitions the disk to DELETING.
	require.NoError(t, target.Cleanup(ctx))
	disk = getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.DELETING, disk.Status)
	assert.Empty(t, allTestDiskVolumes(t, ctx, es.EAC))
}

// TestCreateDiskAndVolume_CleanupDoesNotHardDeleteDisk pins the key behavioral
// change: Cleanup no longer deletes the disk entity (the old r.eac.Delete); it
// keeps the disk alive in DELETING so the controller's handleDeletion path can
// run. A get after cleanup must succeed, and re-running cleanup is a no-op.
func TestCreateDiskAndVolume_CleanupDoesNotHardDeleteDisk(t *testing.T) {
	ctx := t.Context()
	es, resolver := setupResolver(t, nil)

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)
	disks := listTestDisks(t, ctx, es.EAC)
	diskID := disks[0].ID

	require.NoError(t, target.Cleanup(ctx))

	// Disk entity still present (not deleted) and now DELETING.
	disk := getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.DELETING, disk.Status)

	// And re-running cleanup is a no-op (idempotent): patching an already
	// DELETING disk leaves it DELETING.
	require.NoError(t, target.Cleanup(ctx))
	disk = getTestDisk(t, ctx, es.EAC, diskID)
	assert.Equal(t, storage_v1alpha.DELETING, disk.Status)
}

// writeTestImage stands in for the restore having renamed its image into
// place: disk_restore.go only removes the temp file, so once the rename
// commits, this is the file nothing else will reclaim.
func writeTestImage(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("disk image"), 0o644))
}

// TestCreateDiskAndVolume_CleanupRemovesRestoredImage covers the other half of
// the rollback. Cleanup runs only when Finalize did not complete, and with the
// reordered Finalize that means no disk_volume was ever committed — so nothing
// owns the image at target.ImagePath and no controller will ever reclaim it.
// Cleanup has to remove it itself or the failed restore leaks a full-size disk
// image for good.
func TestCreateDiskAndVolume_CleanupRemovesRestoredImage(t *testing.T) {
	ctx := t.Context()
	es, resolver := setupResolver(t, nil)

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)
	diskID := listTestDisks(t, ctx, es.EAC)[0].ID

	writeTestImage(t, target.ImagePath)

	require.NoError(t, target.Cleanup(ctx))

	_, statErr := os.Stat(target.ImagePath)
	assert.True(t, os.IsNotExist(statErr), "the restored image must be removed, got %v", statErr)
	assert.Equal(t, storage_v1alpha.DELETING, getTestDisk(t, ctx, es.EAC, diskID).Status)
}

// TestCreateDiskAndVolume_CleanupToleratesMissingImage guards the removal
// against over-reach: a restore that failed before the rename committed has no
// image to reclaim, and that is not a cleanup failure.
func TestCreateDiskAndVolume_CleanupToleratesMissingImage(t *testing.T) {
	ctx := t.Context()
	es, resolver := setupResolver(t, nil)

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)
	diskID := listTestDisks(t, ctx, es.EAC)[0].ID

	require.NoError(t, target.Cleanup(ctx))
	assert.Equal(t, storage_v1alpha.DELETING, getTestDisk(t, ctx, es.EAC, diskID).Status)
}

// TestCreateDiskAndVolume_CleanupRunsAfterCancellation is the operator-Ctrl-C
// path. disk_restore.go hands Cleanup the restore's own context, which is
// already dead by the time an interrupt triggers the rollback. If cleanup
// inherited that cancellation the disk would stay stuck in RESTORING and block
// every same-name retry, which is the failure the rollback exists to prevent.
func TestCreateDiskAndVolume_CleanupRunsAfterCancellation(t *testing.T) {
	ctx := t.Context()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	_, err := es.Client.Create(ctx, "coordinator", &compute.Node{ApiAddress: ":8444"})
	require.NoError(t, err)

	// The resolver talks through a client that honours cancellation, so a
	// cleanup that inherited the dead context would fail its Patch.
	eac := entityserver_v1alpha.NewEntityAccessClient(cancelAwareRPC{Client: es.EAC.Client})
	resolver := New(eac, entityserver.NewClient(testutils.TestLogger(t), eac))

	target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", 2<<30, "ext4", t.TempDir())
	require.NoError(t, err)
	diskID := listTestDisks(t, ctx, es.EAC)[0].ID

	writeTestImage(t, target.ImagePath)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	require.NoError(t, target.Cleanup(cancelled),
		"cleanup must not inherit the cancellation that triggered it")

	assert.Equal(t, storage_v1alpha.DELETING, getTestDisk(t, ctx, es.EAC, diskID).Status)
	_, statErr := os.Stat(target.ImagePath)
	assert.True(t, os.IsNotExist(statErr), "the restored image must be removed, got %v", statErr)
}

// A disk that claims less capacity than the image it is given is wrong on its
// face, so the size rounds up rather than truncating.
func TestCreateDiskAndVolumeRoundsSizeUp(t *testing.T) {
	ctx := t.Context()

	cases := []struct {
		name      string
		sizeBytes int64
		wantGb    int64
	}{
		{"exactly one GiB", 1 << 30, 1},
		{"a byte over one GiB", (1 << 30) + 1, 2},
		{"one and a half GiB", (1 << 30) * 3 / 2, 2},
		{"exactly two GiB", 2 << 30, 2},
		{"smaller than a GiB still gets one", 4096, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es, resolver := setupResolver(t, nil)

			target, err := resolver.CreateDiskAndVolume(ctx, "mydisk", tc.sizeBytes, "ext4", t.TempDir())
			require.NoError(t, err)
			require.NotNil(t, target)

			disks := listTestDisks(t, ctx, es.EAC)
			require.Len(t, disks, 1)
			assert.Equal(t, tc.wantGb, disks[0].SizeGb)
		})
	}
}
