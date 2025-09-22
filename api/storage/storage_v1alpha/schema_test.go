package storage_v1alpha

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/entity"
)

func TestDisk_Decode(t *testing.T) {
	t.Run("decodes all disk fields", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.String(DiskNameId, "test-disk"),
			entity.Int64(DiskSizeGbId, 100),
			entity.Ref(DiskFilesystemId, DiskFilesystemExt4Id),
			entity.Ref(DiskStatusId, DiskStatusProvisioningId),
			entity.String(DiskLsvdVolumeIdId, "lsvd-vol-abc123"),
			entity.Ref(DiskCreatedById, "app/web-service"),
		)

		e := &entity.Entity{
			ID:    entity.Id("disk/test-disk"),
			Attrs: attrs,
		}
		disk := &Disk{}
		disk.Decode(e)

		assert.Equal(t, entity.Id("disk/test-disk"), disk.ID)
		assert.Equal(t, "test-disk", disk.Name)
		assert.Equal(t, int64(100), disk.SizeGb)
		assert.Equal(t, EXT4, disk.Filesystem)
		assert.Equal(t, PROVISIONING, disk.Status)
		assert.Equal(t, "lsvd-vol-abc123", disk.LsvdVolumeId)
		assert.Equal(t, entity.Id("app/web-service"), disk.CreatedBy)
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.String(DiskNameId, "test-disk"),
			entity.Int64(DiskSizeGbId, 50),
		)

		e := &entity.Entity{
			ID:    entity.Id("disk/test-disk"),
			Attrs: attrs,
		}
		disk := &Disk{}
		disk.Decode(e)

		assert.Equal(t, "test-disk", disk.Name)
		assert.Equal(t, int64(50), disk.SizeGb)
		assert.Equal(t, DiskFilesystem(""), disk.Filesystem) // Empty
		assert.Equal(t, DiskStatus(""), disk.Status)         // Empty
		assert.Empty(t, disk.LsvdVolumeId)
		assert.Empty(t, disk.CreatedBy)
	})
}

func TestDisk_Encode(t *testing.T) {
	t.Run("encodes all disk fields", func(t *testing.T) {
		disk := &Disk{
			ID:           entity.Id("disk/test-disk"),
			Name:         "test-disk",
			SizeGb:       200,
			Filesystem:   XFS,
			Status:       PROVISIONED,
			LsvdVolumeId: "lsvd-vol-xyz456",
			CreatedBy:    entity.Id("app/database"),
		}

		attrs := disk.Encode()

		// Check all attributes are present
		hasName := false
		hasSizeGb := false
		hasFilesystem := false
		hasStatus := false
		hasLsvdVolumeId := false
		hasCreatedBy := false
		hasKind := false

		for _, attr := range attrs {
			switch attr.ID {
			case DiskNameId:
				hasName = true
				assert.Equal(t, "test-disk", attr.Value.String())
			case DiskSizeGbId:
				hasSizeGb = true
				assert.Equal(t, int64(200), attr.Value.Int64())
			case DiskFilesystemId:
				hasFilesystem = true
				assert.Equal(t, entity.Id("dev.miren.storage/filesystem.xfs"), attr.Value.Id())
			case DiskStatusId:
				hasStatus = true
				assert.Equal(t, entity.Id("dev.miren.storage/status.provisioned"), attr.Value.Id())
			case DiskLsvdVolumeIdId:
				hasLsvdVolumeId = true
				assert.Equal(t, "lsvd-vol-xyz456", attr.Value.String())
			case DiskCreatedById:
				hasCreatedBy = true
				assert.Equal(t, entity.Id("app/database"), attr.Value.Id())
			case entity.EntityKind:
				hasKind = true
				assert.Equal(t, KindDisk, attr.Value.Id())
			}
		}

		assert.True(t, hasName, "Name attribute should be present")
		assert.True(t, hasSizeGb, "SizeGb attribute should be present")
		assert.True(t, hasFilesystem, "Filesystem attribute should be present")
		assert.True(t, hasStatus, "Status attribute should be present")
		assert.True(t, hasLsvdVolumeId, "LsvdVolumeId attribute should be present")
		assert.True(t, hasCreatedBy, "CreatedBy attribute should be present")
		assert.True(t, hasKind, "Kind attribute should be present")
	})

	t.Run("omits empty optional fields", func(t *testing.T) {
		disk := &Disk{
			Name:   "minimal-disk",
			SizeGb: 10,
		}

		attrs := disk.Encode()

		// Only required fields and kind should be present
		assert.Len(t, attrs, 3) // name, size_gb, kind
	})
}

func TestDisk_Empty(t *testing.T) {
	t.Run("empty disk", func(t *testing.T) {
		disk := &Disk{}
		assert.True(t, disk.Empty())
	})

	t.Run("non-empty disk", func(t *testing.T) {
		disk := &Disk{Name: "test"}
		assert.False(t, disk.Empty())
	})
}

func TestDiskLease_Decode(t *testing.T) {
	now := time.Now()

	t.Run("decodes all lease fields", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.Ref(DiskLeaseDiskIdId, "disk/test-disk"),
			entity.Ref(DiskLeaseSandboxIdId, "sandbox/test-sandbox"),
			entity.Ref(DiskLeaseAppIdId, "app/test-app"),
			entity.Ref(DiskLeaseStatusId, DiskLeaseStatusPendingId),
			entity.Component(DiskLeaseMountId, []entity.Attr{
				entity.String(MountPathId, "/miren/data/persistent"),
				entity.String(MountOptionsId, "rw,noatime"),
				entity.Bool(MountReadOnlyId, false),
			}),
			entity.Time(DiskLeaseAcquiredAtId, now),
			entity.Ref(DiskLeaseNodeIdId, "node/worker-1"),
			entity.String(DiskLeaseErrorMessageId, "test error"),
		)

		e := &entity.Entity{
			ID:    entity.Id("disk-lease/test-lease"),
			Attrs: attrs,
		}
		lease := &DiskLease{}
		lease.Decode(e)

		assert.Equal(t, entity.Id("disk-lease/test-lease"), lease.ID)
		assert.Equal(t, entity.Id("disk/test-disk"), lease.DiskId)
		assert.Equal(t, entity.Id("sandbox/test-sandbox"), lease.SandboxId)
		assert.Equal(t, entity.Id("app/test-app"), lease.AppId)
		assert.Equal(t, PENDING, lease.Status)
		assert.Equal(t, "/miren/data/persistent", lease.Mount.Path)
		assert.Equal(t, "rw,noatime", lease.Mount.Options)
		assert.False(t, lease.Mount.ReadOnly)
		assert.Equal(t, now.Unix(), lease.AcquiredAt.Unix())
		assert.Equal(t, entity.Id("node/worker-1"), lease.NodeId)
		assert.Equal(t, "test error", lease.ErrorMessage)
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.Ref(DiskLeaseDiskIdId, "disk/test-disk"),
			entity.Ref(DiskLeaseSandboxIdId, "sandbox/test-sandbox"),
			entity.Component(DiskLeaseMountId, []entity.Attr{
				entity.String(MountPathId, "/data"),
			}),
			entity.Time(DiskLeaseAcquiredAtId, time.Now()),
			entity.Ref(DiskLeaseNodeIdId, "node/worker-1"),
		)

		e := &entity.Entity{
			ID:    entity.Id("disk-lease/test-lease"),
			Attrs: attrs,
		}
		lease := &DiskLease{}
		lease.Decode(e)

		assert.Empty(t, lease.AppId)
		assert.Equal(t, DiskLeaseStatus(""), lease.Status)
		assert.Empty(t, lease.ErrorMessage)
		assert.Empty(t, lease.Mount.Options)
		assert.False(t, lease.Mount.ReadOnly)
	})
}

func TestDiskLease_Encode(t *testing.T) {
	now := time.Now()

	t.Run("encodes all lease fields", func(t *testing.T) {
		lease := &DiskLease{
			ID:        entity.Id("disk-lease/test-lease"),
			DiskId:    entity.Id("disk/test-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			AppId:     entity.Id("app/test-app"),
			Status:    BOUND,
			Mount: Mount{
				Path:     "/miren/data/persistent",
				Options:  "rw,noatime",
				ReadOnly: true,
			},
			AcquiredAt:   now,
			NodeId:       entity.Id("node/worker-1"),
			ErrorMessage: "some error",
		}

		attrs := lease.Encode()

		// Verify all fields are encoded
		hasDiskId := false
		hasSandboxId := false
		hasAppId := false
		hasStatus := false
		hasMount := false
		hasAcquiredAt := false
		hasNodeId := false
		hasErrorMessage := false
		hasKind := false

		for _, attr := range attrs {
			switch attr.ID {
			case DiskLeaseDiskIdId:
				hasDiskId = true
			case DiskLeaseSandboxIdId:
				hasSandboxId = true
			case DiskLeaseAppIdId:
				hasAppId = true
			case DiskLeaseStatusId:
				hasStatus = true
				assert.Equal(t, DiskLeaseStatusBoundId, attr.Value.Id())
			case DiskLeaseMountId:
				hasMount = true
			case DiskLeaseAcquiredAtId:
				hasAcquiredAt = true
			case DiskLeaseNodeIdId:
				hasNodeId = true
			case DiskLeaseErrorMessageId:
				hasErrorMessage = true
			case entity.EntityKind:
				hasKind = true
				assert.Equal(t, KindDiskLease, attr.Value.Id())
			}
		}

		assert.True(t, hasDiskId)
		assert.True(t, hasSandboxId)
		assert.True(t, hasAppId)
		assert.True(t, hasStatus)
		assert.True(t, hasMount)
		assert.True(t, hasAcquiredAt)
		assert.True(t, hasNodeId)
		assert.True(t, hasErrorMessage)
		assert.True(t, hasKind)
	})
}

func TestMount_Decode(t *testing.T) {
	t.Run("decodes all mount fields", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.String(MountPathId, "/miren/data/volume"),
			entity.String(MountOptionsId, "rw,noexec"),
			entity.Bool(MountReadOnlyId, true),
		)

		e := &entity.Entity{Attrs: attrs}
		mount := &Mount{}
		mount.Decode(e)

		assert.Equal(t, "/miren/data/volume", mount.Path)
		assert.Equal(t, "rw,noexec", mount.Options)
		assert.True(t, mount.ReadOnly)
	})
}

func TestMount_Encode(t *testing.T) {
	t.Run("encodes all mount fields", func(t *testing.T) {
		mount := &Mount{
			Path:     "/data",
			Options:  "ro",
			ReadOnly: true,
		}

		attrs := mount.Encode()

		assert.Len(t, attrs, 3)
		for _, attr := range attrs {
			switch attr.ID {
			case MountPathId:
				assert.Equal(t, "/data", attr.Value.String())
			case MountOptionsId:
				assert.Equal(t, "ro", attr.Value.String())
			case MountReadOnlyId:
				assert.True(t, attr.Value.Bool())
			}
		}
	})
}

func TestMount_Empty(t *testing.T) {
	t.Run("empty mount", func(t *testing.T) {
		mount := &Mount{}
		assert.True(t, mount.Empty())
	})

	t.Run("non-empty mount with path", func(t *testing.T) {
		mount := &Mount{Path: "/data"}
		assert.False(t, mount.Empty())
	})

	t.Run("non-empty mount with read-only", func(t *testing.T) {
		mount := &Mount{ReadOnly: true}
		assert.False(t, mount.Empty())
	})
}

func TestDisk_Is(t *testing.T) {
	t.Run("identifies disk entity", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.Keyword(entity.Ident, "disk/test"),
			entity.Ref(entity.EntityKind, KindDisk),
		)

		e := &entity.Entity{Attrs: attrs}
		disk := &Disk{}
		assert.True(t, disk.Is(e))
	})

	t.Run("rejects non-disk entity", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.Keyword(entity.Ident, "app/test"),
			entity.Ref(entity.EntityKind, entity.Id("dev.miren.compute/kind.app")),
		)

		e := &entity.Entity{Attrs: attrs}
		disk := &Disk{}
		assert.False(t, disk.Is(e))
	})
}

func TestDiskLease_Is(t *testing.T) {
	t.Run("identifies disk lease entity", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.Keyword(entity.Ident, "disk-lease/test"),
			entity.Ref(entity.EntityKind, KindDiskLease),
		)

		e := &entity.Entity{Attrs: attrs}
		lease := &DiskLease{}
		assert.True(t, lease.Is(e))
	})

	t.Run("rejects non-lease entity", func(t *testing.T) {
		attrs := entity.Attrs(
			entity.Keyword(entity.Ident, "disk/test"),
			entity.Ref(entity.EntityKind, KindDisk),
		)

		e := &entity.Entity{Attrs: attrs}
		lease := &DiskLease{}
		assert.False(t, lease.Is(e))
	})
}

func TestDiskStatus_Values(t *testing.T) {
	// Test all status values are defined
	statuses := []DiskStatus{
		PROVISIONING,
		PROVISIONED,
		ATTACHED,
		DETACHED,
		DELETING,
		ERROR,
	}

	for _, status := range statuses {
		assert.NotEmpty(t, status)
		// Verify mapping exists
		id, ok := diskstatusToId[status]
		assert.True(t, ok, "Status %s should have ID mapping", status)
		assert.NotEmpty(t, id)
	}
}

func TestDiskLeaseStatus_Values(t *testing.T) {
	// Test all lease status values are defined
	statuses := []DiskLeaseStatus{
		PENDING,
		BOUND,
		FAILED,
		RELEASED,
	}

	for _, status := range statuses {
		assert.NotEmpty(t, status)
		// Verify mapping exists
		id, ok := disk_leasestatusToId[status]
		assert.True(t, ok, "Lease status %s should have ID mapping", status)
		assert.NotEmpty(t, id)
	}
}

func TestDiskFilesystem_Values(t *testing.T) {
	// Test all filesystem values are defined
	filesystems := []DiskFilesystem{
		EXT4,
		XFS,
		BTRFS,
	}

	for _, fs := range filesystems {
		assert.NotEmpty(t, fs)
		// Verify mapping exists
		id, ok := diskfilesystemToId[fs]
		assert.True(t, ok, "Filesystem %s should have ID mapping", fs)
		assert.NotEmpty(t, id)
	}
}

func TestKindValues(t *testing.T) {
	assert.Equal(t, entity.Id("dev.miren.storage/kind.disk"), KindDisk)
	assert.Equal(t, entity.Id("dev.miren.storage/kind.disk_lease"), KindDiskLease)
	assert.Equal(t, entity.Id("dev.miren.storage/schema.v1alpha"), Schema)
}

func TestDisk_RoundTrip(t *testing.T) {
	// Test encode -> decode round trip
	original := &Disk{
		Name:         "round-trip-disk",
		SizeGb:       500,
		Filesystem:   BTRFS,
		Status:       ATTACHED,
		LsvdVolumeId: "lsvd-roundtrip",
		CreatedBy:    entity.Id("app/roundtrip"),
	}

	// Encode
	attrs := original.Encode()

	// Decode
	e := &entity.Entity{
		ID:    entity.Id("disk/round-trip"),
		Attrs: attrs,
	}
	decoded := &Disk{}
	decoded.Decode(e)

	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.SizeGb, decoded.SizeGb)
	assert.Equal(t, original.Filesystem, decoded.Filesystem)
	assert.Equal(t, original.Status, decoded.Status)
	assert.Equal(t, original.LsvdVolumeId, decoded.LsvdVolumeId)
	assert.Equal(t, original.CreatedBy, decoded.CreatedBy)
}

func TestDiskLease_RoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)

	// Test encode -> decode round trip
	original := &DiskLease{
		DiskId:    entity.Id("disk/test-disk"),
		SandboxId: entity.Id("sandbox/test-sandbox"),
		AppId:     entity.Id("app/test-app"),
		Status:    FAILED,
		Mount: Mount{
			Path:     "/test/mount",
			Options:  "rw,sync",
			ReadOnly: false,
		},
		AcquiredAt:   now,
		NodeId:       entity.Id("node/test-node"),
		ErrorMessage: "test failure",
	}

	// Encode
	attrs := original.Encode()

	// Decode
	e := &entity.Entity{
		ID:    entity.Id("disk-lease/round-trip"),
		Attrs: attrs,
	}
	decoded := &DiskLease{}
	decoded.Decode(e)

	assert.Equal(t, original.DiskId, decoded.DiskId)
	assert.Equal(t, original.SandboxId, decoded.SandboxId)
	assert.Equal(t, original.AppId, decoded.AppId)
	assert.Equal(t, original.Status, decoded.Status)
	assert.Equal(t, original.Mount.Path, decoded.Mount.Path)
	assert.Equal(t, original.Mount.Options, decoded.Mount.Options)
	assert.Equal(t, original.Mount.ReadOnly, decoded.Mount.ReadOnly)
	assert.Equal(t, original.AcquiredAt.Unix(), decoded.AcquiredAt.Unix())
	assert.Equal(t, original.NodeId, decoded.NodeId)
	assert.Equal(t, original.ErrorMessage, decoded.ErrorMessage)
}
