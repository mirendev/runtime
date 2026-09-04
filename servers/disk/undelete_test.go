package disk

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/diskresolve"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// newUndeleteServer wires a server over a real in-memory entity store, since
// undelete's whole job is entity writes and a fake would not exercise them.
func newUndeleteServer(t *testing.T) (*Server, *testutils.InMemEntityServer, string) {
	t.Helper()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	// One coordinator node, so FindNodeId resolves rather than falling back to
	// whatever was recorded at deletion.
	_, err := es.EAC.Create(context.Background(), entity.New(
		entity.DBId, entity.Id("node/n1"),
		entity.Ref(entity.EntityKind, compute.KindNode),
	).Attrs())
	require.NoError(t, err)

	dataPath := t.TempDir()
	ec := entityserver.NewClient(testutils.TestLogger(t), es.EAC)

	s := &Server{
		log:      slog.Default(),
		disks:    diskresolve.New(es.EAC, ec),
		dataPath: dataPath,
		eac:      es.EAC,
		ec:       ec,
		mntOps:   fakeMountOps{},

		transfers: newKeyedLocks(),
		names:     newKeyedLocks(),
	}
	return s, es, dataPath
}

// seedDeleted puts a volume into the soft-delete holding area the way
// deleteVolume does: the directory, its image, and the metadata describing it.
func seedDeleted(t *testing.T, dataPath, diskName, volumeID string, deletedAt time.Time) string {
	t.Helper()

	diskData := filepath.Join(dataPath, "disk-data")
	dir := filepath.Join(diskio.DeletedVolumesPath(diskData), volumeID)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "disk.img"), []byte("disk contents"), 0644))

	require.NoError(t, diskio.SaveDeletedVolumeMetadata(dir, &diskio.DeletedVolumeMetadata{
		DiskID:     "disk/old-" + volumeID,
		DiskName:   diskName,
		SizeGb:     1,
		Filesystem: "ext4",
		VolumeID:   volumeID,
		VolumeMode: string(storage_v1alpha.VM_UNIVERSAL),
		NodeID:     compute.NodeId("node/n1"),
		DeletedAt:  deletedAt,
	}))
	return dir
}

func TestListDeletedIsEmptyWhenNothingWasDeleted(t *testing.T) {
	s, _, _ := newUndeleteServer(t)

	disks, retention, err := s.listDeleted()
	require.NoError(t, err)
	assert.Empty(t, disks)
	assert.Positive(t, retention, "the client shows an expiry, so retention has to come back")
}

func TestListDeletedReportsNewestFirstWithAnExpiry(t *testing.T) {
	s, _, dataPath := newUndeleteServer(t)

	older := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	newer := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	seedDeleted(t, dataPath, "old-disk", "vol-old", older)
	seedDeleted(t, dataPath, "new-disk", "vol-new", newer)

	disks, retention, err := s.listDeleted()
	require.NoError(t, err)
	require.Len(t, disks, 2)

	// The disk someone wants back is almost always the one they just lost.
	assert.Equal(t, "new-disk", disks[0].DiskName())
	assert.Equal(t, "old-disk", disks[1].DiskName())

	assert.Equal(t, "vol-new", disks[0].VolumeId())
	assert.Equal(t, int64(1), disks[0].SizeGb())
	assert.Equal(t, "ext4", disks[0].Filesystem())

	require.True(t, disks[0].HasDeletedAt())
	assert.Equal(t, newer.Unix(), disks[0].DeletedAt().Seconds())

	// Expiry is what tells an operator how long they have to act on this.
	require.True(t, disks[0].HasExpiresAt())
	wantExpiry := newer.Add(time.Duration(retention) * 24 * time.Hour)
	assert.Equal(t, wantExpiry.Unix(), disks[0].ExpiresAt().Seconds())
}

func TestUndeleteMovesTheVolumeBackAndRecreatesEntities(t *testing.T) {
	s, es, dataPath := newUndeleteServer(t)
	holding := seedDeleted(t, dataPath, "mydisk", "vol-1", time.Now().Add(-time.Hour))

	res, err := s.undelete(context.Background(), "mydisk", "")
	require.NoError(t, err)

	// The data is live again, and gone from the holding area.
	imagePath := filepath.Join(dataPath, "disk-data", "volumes", "vol-1", "disk.img")
	assert.FileExists(t, imagePath)
	assert.NoDirExists(t, holding)
	assert.Equal(t, imagePath, res.imagePath)

	// The metadata only described the volume while it sat in the holding area.
	_, statErr := os.Stat(filepath.Join(filepath.Dir(imagePath), "metadata.json"))
	assert.True(t, os.IsNotExist(statErr), "metadata.json should be gone once the volume is live")

	disks := listDisks(t, es)
	require.Len(t, disks, 1)
	// PROVISIONING, not PROVISIONED: the DiskController promotes it only once
	// the volume is actually mounted, so it never reports itself leasable early.
	assert.Equal(t, storage_v1alpha.PROVISIONING, disks[0].Status)
	assert.Equal(t, "vol-1", disks[0].VolumeId)

	vols := listVolumes(t, es)
	require.Len(t, vols, 1)
	assert.Equal(t, storage_v1alpha.DV_PENDING, vols[0].ActualState)
	assert.Equal(t, storage_v1alpha.DV_PRESENT, vols[0].DesiredState)
	assert.Equal(t, imagePath, vols[0].ImagePath)
	assert.Equal(t, entity.Id("node/n1"), vols[0].NodeId)
}

// The recovered disk must not reuse the id of the disk that was deleted, or
// anything still pointing at that id gets silently reconnected.
func TestUndeleteGivesTheDiskAFreshId(t *testing.T) {
	s, es, dataPath := newUndeleteServer(t)
	seedDeleted(t, dataPath, "mydisk", "vol-1", time.Now())

	_, err := s.undelete(context.Background(), "mydisk", "")
	require.NoError(t, err)

	disks := listDisks(t, es)
	require.Len(t, disks, 1)
	assert.NotEqual(t, entity.Id("disk/old-vol-1"), disks[0].ID)
}

func TestUndeleteReportsAnUnknownName(t *testing.T) {
	s, _, _ := newUndeleteServer(t)

	_, err := s.undelete(context.Background(), "nope", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no deleted disk found named "nope"`)
}

// Two deletions of the same name are ordinary — delete, recreate, delete again
// — so the ambiguity has to be reported with the ids needed to resolve it.
func TestUndeleteNamesTheChoicesWhenSeveralShareAName(t *testing.T) {
	s, _, dataPath := newUndeleteServer(t)
	seedDeleted(t, dataPath, "mydisk", "vol-1", time.Now().Add(-2*time.Hour))
	seedDeleted(t, dataPath, "mydisk", "vol-2", time.Now().Add(-time.Hour))

	_, err := s.undelete(context.Background(), "mydisk", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vol-1")
	assert.Contains(t, err.Error(), "vol-2")

	// Naming one resolves it.
	res, err := s.undelete(context.Background(), "mydisk", "vol-2")
	require.NoError(t, err)
	assert.Equal(t, "vol-2", res.volumeID)
}

func TestUndeleteRefusesWhenALiveDiskHoldsTheName(t *testing.T) {
	s, _, dataPath := newUndeleteServer(t)
	seedDeleted(t, dataPath, "mydisk", "vol-1", time.Now())

	// A live disk already answering to the name.
	_, err := s.ec.Create(context.Background(), "disk/live", &storage_v1alpha.Disk{
		Name:       "mydisk",
		SizeGb:     1,
		Filesystem: storage_v1alpha.EXT4,
		Status:     storage_v1alpha.PROVISIONED,
	})
	require.NoError(t, err)

	_, err = s.undelete(context.Background(), "mydisk", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// A holding-area directory with no image is corrupt. Recovery must roll back
// rather than leave entities pointing at data that is not there.
func TestUndeleteRollsBackWhenTheImageIsMissing(t *testing.T) {
	s, es, dataPath := newUndeleteServer(t)
	holding := seedDeleted(t, dataPath, "mydisk", "vol-1", time.Now())
	require.NoError(t, os.Remove(filepath.Join(holding, "disk.img")))

	_, err := s.undelete(context.Background(), "mydisk", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may be corrupted")

	// The data went back to the holding area, so the operator can try again.
	assert.DirExists(t, holding)
	assert.NoDirExists(t, filepath.Join(dataPath, "disk-data", "volumes", "vol-1"))

	// And no disk entity was left behind.
	assert.Empty(t, listDisks(t, es))
}

func listDisks(t *testing.T, es *testutils.InMemEntityServer) []storage_v1alpha.Disk {
	t.Helper()
	resp, err := es.EAC.List(context.Background(), entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk))
	require.NoError(t, err)

	var out []storage_v1alpha.Disk
	for _, v := range resp.Values() {
		var d storage_v1alpha.Disk
		d.Decode(v.Entity())
		out = append(out, d)
	}
	return out
}

func listVolumes(t *testing.T, es *testutils.InMemEntityServer) []storage_v1alpha.DiskVolume {
	t.Helper()
	resp, err := es.EAC.List(context.Background(), entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskVolume))
	require.NoError(t, err)

	var out []storage_v1alpha.DiskVolume
	for _, v := range resp.Values() {
		var vol storage_v1alpha.DiskVolume
		vol.Decode(v.Entity())
		out = append(out, vol)
	}
	return out
}

// The name check and the create that follows it are two steps, so two
// recoveries of the same name must not both find it free. Two disks answering
// to one name make every lookup by that name ambiguous from then on.
func TestUndeleteSerializesRecoveriesOfTheSameName(t *testing.T) {
	s, es, dataPath := newUndeleteServer(t)
	seedDeleted(t, dataPath, "mydisk", "vol-1", time.Now())

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.undelete(context.Background(), "mydisk", "")
		}()
	}
	wg.Wait()

	// One recovers it; the other finds nothing left in the holding area, or
	// finds the name taken. Either way it must not create a second disk.
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	assert.Equal(t, 1, succeeded, "exactly one recovery should have succeeded")

	disks := listDisks(t, es)
	assert.Len(t, disks, 1, "a name must never end up on two disks")
}
