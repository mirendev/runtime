package snapshot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockResolver struct {
	disk    *DiskState
	diskErr error

	volume    *VolumeState
	volumeErr error

	leases    []LeaseState
	leasesErr error
}

func (m *mockResolver) FindDisk(_ context.Context, _ string) (*DiskState, error) {
	if m.diskErr != nil {
		return nil, m.diskErr
	}
	return m.disk, nil
}

func (m *mockResolver) FindVolume(_ context.Context, _ string) (*VolumeState, error) {
	if m.volumeErr != nil {
		return nil, m.volumeErr
	}
	return m.volume, nil
}

func (m *mockResolver) FindLeases(_ context.Context, _ string) ([]LeaseState, error) {
	if m.leasesErr != nil {
		return nil, m.leasesErr
	}
	return m.leases, nil
}

func TestPrepareBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED", Filesystem: "ext4"},
			volume: &VolumeState{VolumeID: "v1", ImagePath: "/data/disk.img"},
		}
		target, err := PrepareBackup(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
		assert.Equal(t, "mydb", target.Name)
		assert.Equal(t, "ext4", target.Filesystem)
		assert.Equal(t, "/data/disk.img", target.ImagePath)
	})

	t.Run("deleting disk rejected", func(t *testing.T) {
		r := &mockResolver{
			disk: &DiskState{ID: "d1", Name: "mydb", Status: StatusDeleting},
		}
		_, err := PrepareBackup(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "being deleted")
		assert.Contains(t, err.Error(), "mydb")
	})

	t.Run("disk not found", func(t *testing.T) {
		r := &mockResolver{
			diskErr: fmt.Errorf("disk %q not found", "missing"),
		}
		_, err := PrepareBackup(ctx, r, "missing", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("volume not found", func(t *testing.T) {
		r := &mockResolver{
			disk:      &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			volumeErr: fmt.Errorf("no disk volume found for disk d1"),
		}
		_, err := PrepareBackup(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no disk volume")
	})

	t.Run("default image path from volume ID", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED", Filesystem: "ext4"},
			volume: &VolumeState{VolumeID: "vol-abc", ImagePath: ""},
		}
		target, err := PrepareBackup(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
		assert.Equal(t, "/var/lib/miren/disk-data/volumes/vol-abc/disk.img", target.ImagePath)
	})

	t.Run("explicit image path takes precedence", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED", Filesystem: "xfs"},
			volume: &VolumeState{VolumeID: "vol-abc", ImagePath: "/custom/path/image.raw"},
		}
		target, err := PrepareBackup(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
		assert.Equal(t, "/custom/path/image.raw", target.ImagePath)
	})
}

func TestPrepareRestore(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases: nil,
			volume: &VolumeState{VolumeID: "v1", ImagePath: "/data/disk.img"},
		}
		target, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
		assert.Equal(t, "mydb", target.Name)
		assert.Equal(t, "/data/disk.img", target.ImagePath)
	})

	t.Run("deleting disk rejected", func(t *testing.T) {
		r := &mockResolver{
			disk: &DiskState{ID: "d1", Name: "mydb", Status: StatusDeleting},
		}
		_, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "being deleted")
		assert.Contains(t, err.Error(), "mydb")
	})

	t.Run("bound lease rejected", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases: []LeaseState{{ID: "lease-1", Status: LeaseStatusBound}},
		}
		_, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "active lease")
		assert.Contains(t, err.Error(), "lease-1")
	})

	t.Run("released lease allowed", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases: []LeaseState{{ID: "lease-1", Status: "RELEASED"}},
			volume: &VolumeState{VolumeID: "v1", ImagePath: "/data/disk.img"},
		}
		target, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
		assert.Equal(t, "/data/disk.img", target.ImagePath)
	})

	t.Run("multiple leases one bound", func(t *testing.T) {
		r := &mockResolver{
			disk: &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases: []LeaseState{
				{ID: "lease-1", Status: "RELEASED"},
				{ID: "lease-2", Status: LeaseStatusBound},
				{ID: "lease-3", Status: "PENDING"},
			},
		}
		_, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "lease-2")
	})

	t.Run("no leases allowed", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases: []LeaseState{},
			volume: &VolumeState{VolumeID: "v1", ImagePath: "/data/disk.img"},
		}
		_, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
	})

	t.Run("disk not found", func(t *testing.T) {
		r := &mockResolver{
			diskErr: fmt.Errorf("disk %q not found", "missing"),
		}
		_, err := PrepareRestore(ctx, r, "missing", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("lease query error", func(t *testing.T) {
		r := &mockResolver{
			disk:      &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leasesErr: fmt.Errorf("connection refused"),
		}
		_, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})

	t.Run("volume not found", func(t *testing.T) {
		r := &mockResolver{
			disk:      &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases:    nil,
			volumeErr: fmt.Errorf("no disk volume found for disk d1"),
		}
		_, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no disk volume")
	})

	t.Run("default image path from volume ID", func(t *testing.T) {
		r := &mockResolver{
			disk:   &DiskState{ID: "d1", Name: "mydb", Status: "PROVISIONED"},
			leases: nil,
			volume: &VolumeState{VolumeID: "vol-xyz", ImagePath: ""},
		}
		target, err := PrepareRestore(ctx, r, "mydb", "/var/lib/miren")
		require.NoError(t, err)
		assert.Equal(t, "/var/lib/miren/disk-data/volumes/vol-xyz/disk.img", target.ImagePath)
	})
}
