package runner

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/controllers/disk"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

func TestDiskControllersCanBeCreated(t *testing.T) {
	t.Run("disk and disk lease controllers can be instantiated", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()
		log := slog.Default()

		// Create a mock LSVD client
		dataPath := "/tmp/test-disks"
		lsvdClient := disk.NewLsvdClient(log, dataPath)
		r.NotNil(lsvdClient)

		// Create disk controller
		diskController := disk.NewDiskController(log, nil, lsvdClient)
		r.NotNil(diskController)

		// Create disk lease controller
		diskLeaseController := disk.NewDiskLeaseController(log, nil, nil)
		r.NotNil(diskLeaseController)

		// Verify controllers can be initialized
		err := diskController.Init(ctx)
		r.NoError(err)

		err = diskLeaseController.Init(ctx)
		r.NoError(err)
	})

	t.Run("disk controllers can be adapted for reconciliation", func(t *testing.T) {
		r := require.New(t)
		log := slog.Default()

		// Create controllers
		dataPath := "/tmp/test-disks"
		lsvdClient := disk.NewLsvdClient(log, dataPath)
		diskController := disk.NewDiskController(log, nil, lsvdClient)
		diskLeaseController := disk.NewDiskLeaseController(log, nil, nil)

		// Verify they can be adapted for the controller manager
		diskAdapter := controller.AdaptController(diskController)
		r.NotNil(diskAdapter)

		diskLeaseAdapter := controller.AdaptController(diskLeaseController)
		r.NotNil(diskLeaseAdapter)
	})

	t.Run("disk controller references are valid", func(t *testing.T) {
		r := require.New(t)

		// Verify entity references work correctly
		diskRef := entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk)
		r.NotNil(diskRef)

		diskLeaseRef := entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease)
		r.NotNil(diskLeaseRef)

		// Verify we can create reconcile controllers with these references (without starting them)
		log := slog.Default()

		rc1 := controller.NewReconcileController(
			"disk",
			log,
			diskRef,
			nil, // EAC can be nil for this test
			nil, // Controller can be nil for this test
			time.Minute,
			1,
		)
		r.NotNil(rc1)

		rc2 := controller.NewReconcileController(
			"disk-lease",
			log,
			diskLeaseRef,
			nil, // EAC can be nil for this test
			nil, // Controller can be nil for this test
			time.Minute,
			1,
		)
		r.NotNil(rc2)
	})
}
