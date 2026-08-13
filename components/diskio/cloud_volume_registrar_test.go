package diskio

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/entity/testutils"
)

// writeStubAuth answers the service-account challenge/response handshake so a
// test server can get as far as the request it actually cares about.
func writeStubAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/begin") {
		json.NewEncoder(w).Encode(cloudauth.BeginAuthResponse{
			Envelope:  "test-envelope",
			Challenge: "test-challenge",
		})
		return
	}
	json.NewEncoder(w).Encode(cloudauth.CompleteAuthResponse{
		Token:     "test-jwt-token",
		ExpiresIn: 3600,
	})
}

// stubRegistrar records what it was asked to register and hands back a
// canned cloud id, or an error when one is set.
type stubRegistrar struct {
	calls    []RegisterVolumeRequest
	volumeID string
	err      error
}

func (s *stubRegistrar) EnsureVolume(ctx context.Context, req RegisterVolumeRequest) (string, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return "", s.err
	}
	return s.volumeID, nil
}

func TestCloudVolumeNameIsScopedToCluster(t *testing.T) {
	// Two clusters holding a disk with the same local id must not collide on
	// one cloud volume and interleave their backups.
	assert.Equal(t, "cluster-a-vol-1", cloudVolumeName("cluster-a", "vol-1"))
	assert.Equal(t, "cluster-b-vol-1", cloudVolumeName("cluster-b", "vol-1"))
	assert.Equal(t, "vol-1", cloudVolumeName("", "vol-1"))
}

func TestDeterministicVolumeUUIDIsStableAndDistinct(t *testing.T) {
	first := deterministicVolumeUUID("cluster-a", "vol-1")
	again := deterministicVolumeUUID("cluster-a", "vol-1")

	// The cloud enforces a globally unique UUID, so a fresh one per attempt
	// would turn every retry into a constraint violation.
	assert.Equal(t, first, again)
	assert.NotEqual(t, first, deterministicVolumeUUID("cluster-b", "vol-1"))
	assert.NotEqual(t, first, deterministicVolumeUUID("cluster-a", "vol-2"))

	assert.Len(t, first, 36)
	assert.Equal(t, byte('4'), first[14], "should be stamped as a version 4 UUID")
	assert.Contains(t, "89ab", string(first[19]), "should carry the RFC 4122 variant")
}

func TestCloudVolumeRegistrarPostsStableIdentity(t *testing.T) {
	var got createVolumeRequestJSON

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/auth/service-account/") {
			writeStubAuth(w, r)
			return
		}
		require.Equal(t, "/api/v1/disk/volumes", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "Bearer test-jwt-token", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		json.NewEncoder(w).Encode(createVolumeResponseJSON{VolumeID: "cloud-vol-abc"})
	}))
	defer srv.Close()

	registrar := NewCloudVolumeRegistrar(slog.Default(), srv.URL, newTestAuthClient(t, srv.URL))

	id, err := registrar.EnsureVolume(context.Background(), RegisterVolumeRequest{
		LocalVolumeID: "vol-1",
		ClusterID:     "cluster-a",
		DisplayName:   "data",
		SizeBytes:     10 << 30,
		Filesystem:    "ext4",
	})
	require.NoError(t, err)
	assert.Equal(t, "cloud-vol-abc", id)

	assert.Equal(t, "cluster-a-vol-1", got.Name)
	assert.Equal(t, deterministicVolumeUUID("cluster-a", "vol-1"), got.UUID)
	assert.Equal(t, int64(10<<30), got.DeclaredSize)
	assert.Equal(t, "ext4", got.AppFormat)
	assert.Equal(t, "cluster-a", got.Metadata["cluster_id"])
	assert.Equal(t, "vol-1", got.Metadata["local_volume_id"])
	assert.Equal(t, "data", got.Metadata["disk_name"])
}

func TestCloudVolumeRegistrarRejectsIncompleteRequests(t *testing.T) {
	registrar := NewCloudVolumeRegistrar(slog.Default(), "http://unused", nil)

	_, err := registrar.EnsureVolume(context.Background(), RegisterVolumeRequest{SizeBytes: 1})
	require.ErrorContains(t, err, "local volume ID is required")

	_, err = registrar.EnsureVolume(context.Background(), RegisterVolumeRequest{LocalVolumeID: "vol-1"})
	require.ErrorContains(t, err, "size must be positive")
}

func TestCloudVolumeRegistrarSurfacesServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/auth/service-account/") {
			writeStubAuth(w, r)
			return
		}
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"uuid already in use"}`)
	}))
	defer srv.Close()

	registrar := NewCloudVolumeRegistrar(slog.Default(), srv.URL, newTestAuthClient(t, srv.URL))

	_, err := registrar.EnsureVolume(context.Background(), RegisterVolumeRequest{
		LocalVolumeID: "vol-1",
		SizeBytes:     1 << 20,
	})
	require.ErrorContains(t, err, "status 409")
	require.ErrorContains(t, err, "uuid already in use")
}

// reconcileRegisteredVolume drives a full reconcile of one pending volume and
// returns the controller's state for it.
func reconcileRegisteredVolume(t *testing.T, registrar CloudVolumeRegistrar, clusterID string) (*VolumeState, *storage_v1alpha.DiskVolume) {
	t.Helper()
	ctx := t.Context()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	state := NewState()
	vc := newTestDiskVolumeController(testutils.TestLogger(t), t.TempDir(), "test-node-1",
		es.EAC, state, newMockDiskVolumeOps())
	if registrar != nil {
		vc.SetCloudVolumeRegistrar(registrar, clusterID)
	}

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-123",
		NodeId:       compute.NewNodeId("test-node-1").Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		Name:         "data",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	require.NoError(t, vc.ReconcileWithEntities(ctx))

	resp, err := es.EAC.Get(ctx, "disk_volume/vol-123")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskVolume
	updated.Decode(resp.Entity().Entity())

	return state.GetVolume("disk_volume/vol-123"), &updated
}

func TestReconcileRegistersVolumeWithCloud(t *testing.T) {
	registrar := &stubRegistrar{volumeID: "cloud-vol-abc"}

	volState, entityVol := reconcileRegisteredVolume(t, registrar, "cluster-a")

	require.Len(t, registrar.calls, 1)
	assert.Equal(t, "vol-123", registrar.calls[0].LocalVolumeID)
	assert.Equal(t, "cluster-a", registrar.calls[0].ClusterID)
	assert.Equal(t, "data", registrar.calls[0].DisplayName)
	assert.Equal(t, int64(10<<30), registrar.calls[0].SizeBytes)

	// The local id still names the mount point; the cloud id sits beside it.
	require.NotNil(t, volState)
	assert.Equal(t, "vol-123", volState.VolumeId)
	assert.Equal(t, "cloud-vol-abc", volState.CloudVolumeId)

	// Recorded on the entity too, so a node that loses its state file can
	// recover the id instead of registering a second volume for the same disk.
	assert.Equal(t, "cloud-vol-abc", entityVol.CloudVolumeId)
	assert.Empty(t, entityVol.MountId,
		"mount_id keeps its documented meaning as a mount-point override")
}

func TestReconcileSurvivesRegistrationFailure(t *testing.T) {
	registrar := &stubRegistrar{err: fmt.Errorf("cloud unreachable")}

	// A cloud we cannot reach must not stop a local disk from working.
	volState, _ := reconcileRegisteredVolume(t, registrar, "cluster-a")

	require.NotNil(t, volState)
	assert.Equal(t, "vol-123", volState.VolumeId)
	assert.Empty(t, volState.CloudVolumeId)
}

func TestReconcileWithoutCloudLeavesVolumeLocal(t *testing.T) {
	volState, entityVol := reconcileRegisteredVolume(t, nil, "")

	require.NotNil(t, volState)
	assert.Equal(t, "vol-123", volState.VolumeId)
	assert.Empty(t, volState.CloudVolumeId)
	assert.Empty(t, entityVol.CloudVolumeId)
}

func TestReconcileDoesNotReRegisterKnownVolume(t *testing.T) {
	ctx := t.Context()

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	state := NewState()
	registrar := &stubRegistrar{volumeID: "cloud-vol-abc"}
	vc := newTestDiskVolumeController(testutils.TestLogger(t), t.TempDir(), "test-node-1",
		es.EAC, state, newMockDiskVolumeOps())
	vc.SetCloudVolumeRegistrar(registrar, "cluster-a")

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-123",
		NodeId:       compute.NewNodeId("test-node-1").Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	require.NoError(t, vc.ReconcileWithEntities(ctx))
	require.NoError(t, vc.ReconcileWithEntities(ctx))
	require.NoError(t, vc.ReconcileWithEntities(ctx))

	// Registration is idempotent cloud-side, but a call per reconcile tick for
	// every volume forever is still waste worth not spending.
	assert.Len(t, registrar.calls, 1)
}

// mount_id is documented as an override for the mount point directory name.
// Cloud registration must not quietly take that field over, or a volume that
// set it would find its live mount relocated.
func TestMountIdStillOverridesTheMountPoint(t *testing.T) {
	ctx := t.Context()

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	state := NewState()
	registrar := &stubRegistrar{volumeID: "cloud-vol-abc"}
	vc := newTestDiskVolumeController(testutils.TestLogger(t), t.TempDir(), "test-node-1",
		es.EAC, state, newMockDiskVolumeOps())
	vc.SetCloudVolumeRegistrar(registrar, "cluster-a")

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-123",
		NodeId:       compute.NewNodeId("test-node-1").Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		MountId:      "custom-mount",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	require.NoError(t, vc.ReconcileWithEntities(ctx))

	volState := state.GetVolume("disk_volume/vol-123")
	require.NotNil(t, volState)
	assert.Equal(t, "custom-mount", volState.VolumeId,
		"the mount point keeps following mount_id")
	assert.Equal(t, "cloud-vol-abc", volState.CloudVolumeId,
		"and the cloud id lands in its own field")

	// The registrar is told the local id, which is what names the mount
	require.Len(t, registrar.calls, 1)
	assert.Equal(t, "custom-mount", registrar.calls[0].LocalVolumeID)
}
