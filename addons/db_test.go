package addons

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/testutils"
)

func TestDB(t *testing.T) {
	t.Run("manipulates addon instances", func(t *testing.T) {
		r := require.New(t)

		reg, cleanup := testutils.Registry()
		defer cleanup()

		var db DB

		err := reg.Populate(&db)
		r.NoError(err)

		ctx := context.Background()

		tx, err := db.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		db.UseTx(tx)

		// Create test organization
		var orgId uint64
		err = tx.QueryRow(ctx, `
			INSERT INTO organizations (name, external_id)
			VALUES ('test-org', 'ext-org-123')
			RETURNING id
		`).Scan(&orgId)
		r.NoError(err)

		// Create test disks
		var diskIds []uint64
		for i := 1; i <= 2; i++ {
			var diskId uint64
			err = tx.QueryRow(ctx, `
				INSERT INTO disks (organization_id, name, external_id, capacity)
				VALUES ($1, $2, $3, $4)
				RETURNING id
			`, orgId,
				"test-disk-"+string(rune('0'+i)),
				"ext-disk-"+string(rune('0'+i)),
				1024*1024*1024).Scan(&diskId)
			r.NoError(err)
			diskIds = append(diskIds, diskId)
		}

		// Create test instance
		instance := &Instance{
			Xid:         "ext-123",
			ContainerId: "container-123",
			DiskIds:     diskIds,
		}

		err = db.CreateInstance(instance)
		r.NoError(err)
		r.NotZero(instance.Id, "should set instance ID")
		r.NotZero(instance.CreatedAt, "should set created timestamp")
		r.NotZero(instance.UpdatedAt, "should set updated timestamp")

		// Get instance
		retrieved, err := db.GetInstance(instance.Id)
		r.NoError(err)

		r.Equal(instance.Xid, retrieved.Xid)
		r.Equal(instance.ContainerId, retrieved.ContainerId)
		r.Equal(instance.DiskIds, retrieved.DiskIds)
		r.Equal(instance.CreatedAt, retrieved.CreatedAt)
		r.Equal(instance.UpdatedAt, retrieved.UpdatedAt)

		// Delete instance
		err = db.DeleteInstance(instance.Id)
		r.NoError(err)

		// Verify deletion
		_, err = db.GetInstance(instance.Id)
		r.Error(err, "should return error for deleted instance")
	})

	t.Run("handles missing instances", func(t *testing.T) {
		r := require.New(t)

		reg, cleanup := testutils.Registry()
		defer cleanup()

		var db DB

		err := reg.Populate(&db)
		r.NoError(err)

		ctx := context.Background()

		tx, err := db.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		// Try to get non-existent instance
		_, err = db.GetInstance(999999)
		r.Error(err, "should return error for non-existent instance")

		// Try to delete non-existent instance
		err = db.DeleteInstance(999999)
		r.Error(err, "should return error for non-existent instance")
	})

	t.Run("handles optional relationships", func(t *testing.T) {
		r := require.New(t)

		reg, cleanup := testutils.Registry()
		defer cleanup()

		var db DB

		err := reg.Populate(&db)
		r.NoError(err)

		ctx := context.Background()

		tx, err := db.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		// Create instance without disks or app
		instance := &Instance{
			Xid:         "ext-123",
			ContainerId: "container-123",
		}

		err = db.CreateInstance(instance)
		r.NoError(err)

		// Get instance
		retrieved, err := db.GetInstance(instance.Id)
		r.NoError(err)

		r.Empty(retrieved.DiskIds, "should have no disk IDs")
	})
}
