package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/controllers/disk"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/multierror"
)

// NodeStorage owns local disk recovery and the controllers that maintain disk
// state for one runner. Start recovers the durable state but deliberately does
// not begin watching desired state; StorageAgent owns that later transition.
type NodeStorage struct {
	config RunnerConfig
	deps   RunnerDeps
	access *ClusterAccess

	dvc     *diskio.DiskVolumeController
	dmc     *diskio.DiskMountController
	diskGC  *diskio.DeletedVolumeGC
	manager *controller.ControllerManager
	closers []io.Closer
}

// NewNodeStorage constructs durable node storage on top of cluster access.
func NewNodeStorage(access *ClusterAccess, deps RunnerDeps, config RunnerConfig) (*NodeStorage, error) {
	if access == nil {
		return nil, fmt.Errorf("cluster access is required")
	}
	return &NodeStorage{access: access, deps: deps, config: config}, nil
}

func (s *NodeStorage) nodeId() compute_v1alpha.NodeId {
	return compute_v1alpha.NewNodeId(s.config.Id)
}

func (s *NodeStorage) Start(ctx context.Context) error {
	if s.access.eac == nil {
		return fmt.Errorf("cluster access is not ready")
	}
	eas := s.access.eac
	log := s.access.Log
	workers := s.config.Workers
	if workers <= 0 {
		workers = DefaulWorkers
	}
	manager := controller.NewControllerManager()

	dataPath := filepath.Join(s.config.DataPath, "disk-data")
	if err := os.MkdirAll(dataPath, 0700); err != nil {
		return fmt.Errorf("failed to create disk data path: %w", err)
	}
	if err := diskio.EnsureLoopDevices(log); err != nil {
		log.Warn("Loop devices not available, disk mounts will fail", "error", err)
	}
	if err := diskio.EnsureLbdDevices(log); err != nil {
		log.Warn("lbd devices not available, accelerator mode will not work", "error", err)
	}

	diskioState, err := diskio.LoadState(dataPath)
	if err != nil {
		log.Warn("failed to load disk state, starting fresh", "error", err)
		diskioState = diskio.NewState()
		diskioState.SetPath(dataPath)
	}
	volOps := diskio.NewRealDiskVolumeOps(log)
	mntOps := diskio.NewRealDiskMountOps(log)

	s.dvc = diskio.NewDiskVolumeController(log, dataPath, s.nodeId(), diskioState, volOps, mntOps)
	s.dvc.SetEAC(eas)
	if err := s.dvc.Init(ctx); err != nil {
		return fmt.Errorf("disk volume controller init: %w", err)
	}
	s.dmc = diskio.NewDiskMountController(log, dataPath, s.nodeId(), diskioState, mntOps)
	s.dmc.SetEAC(eas)
	s.diskGC = &diskio.DeletedVolumeGC{
		Log:      log.With("module", "deleted-volume-gc"),
		DataPath: dataPath,
		Config:   diskio.DefaultDeletedVolumeGCConfig(),
	}
	s.closers = append(s.closers, shutdownCloser{s.dvc}, shutdownCloser{s.dmc})

	var logUploader diskio.LogSegmentUploader
	if auth := s.config.CloudAuth; auth != nil && auth.Enabled && auth.PrivateKey != "" {
		cloudURL := auth.CloudURL
		if cloudURL == "" {
			cloudURL = coordinate.DefaultCloudURL
		}
		var keyData []byte
		if strings.HasPrefix(auth.PrivateKey, "-----BEGIN PRIVATE KEY-----") {
			keyData = []byte(auth.PrivateKey)
		} else if loaded, readErr := os.ReadFile(auth.PrivateKey); readErr != nil {
			log.Warn("failed to load cloud auth private key for log watcher", "error", readErr)
		} else {
			keyData = loaded
		}
		if keyData != nil {
			keyPair, keyErr := cloudauth.LoadKeyPairFromPEM(string(keyData))
			if keyErr != nil {
				log.Warn("failed to parse cloud auth private key for log watcher", "error", keyErr)
			} else if authClient, authErr := cloudauth.NewAuthClient(cloudURL, keyPair); authErr != nil {
				log.Warn("failed to create auth client for log watcher", "error", authErr)
			} else {
				updates := diskio.NewCloudUpdatesClient(log, cloudURL, authClient)
				s.dmc.SetCloudClient(diskio.NewCloudDiskClientWithUpdates(log, cloudURL, authClient, updates))
				s.dmc.SetUpdatesClient(updates)
				s.dvc.SetUpdatesClient(updates)
				s.dvc.SetCloudVolumeRegistrar(diskio.NewCloudVolumeRegistrar(log, cloudURL, authClient), auth.ClusterID)
				logUploader = diskio.NewCloudSegmentUploaderWithClient(log, updates, diskioState)
			}
		}
	}

	if err := s.dvc.ReconcileWithEntities(ctx); err != nil {
		log.Warn("failed to reconcile disk volumes on startup", "error", err)
	}
	if err := s.dmc.ReconcileWithEntities(ctx); err != nil {
		log.Warn("failed to reconcile disk mounts on startup", "error", err)
	}

	watcher := diskio.NewLogWatcher(log, diskioState, logUploader, 5*time.Second)
	go func() {
		if err := watcher.Run(ctx); err != nil {
			log.Error("log watcher stopped", "error", err)
		}
	}()
	s.closers = append(s.closers, waitCloser{watcher})

	volumeHandler := controller.AdaptReconcileController[storage_v1alpha.DiskVolume](s.dvc)
	manager.AddController(controller.NewReconcileController(
		"disk-volume", log, s.dvc.Index(), eas, volumeHandler, 5*time.Minute, workers,
	))
	mountHandler := controller.AdaptReconcileController[storage_v1alpha.DiskMount](s.dmc)
	mountController := controller.NewReconcileController(
		"disk-mount", log, s.dmc.Index(), eas, mountHandler, 5*time.Minute, workers,
	)
	s.dmc.SetWriteTracker(mountController.WriteTracker())
	mountController.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		if err := s.dmc.ReconcileOrphanMounts(ctx, diskio.OrphanMountSweepGracePeriod); err != nil {
			log.Warn("periodic orphan mount sweep failed", "error", err)
		}
		return nil
	})
	manager.AddController(mountController)

	diskController := disk.NewDiskController(log, eas, s.nodeId(), s.config.DiskMode, s.deps.IsCoordinator)
	diskLeaseController := disk.NewDiskLeaseController(log, eas, s.nodeId(), s.config.DiskMode)
	s.closers = append(s.closers, diskController)
	if err := diskController.Init(ctx); err != nil {
		return err
	}
	if err := diskLeaseController.Init(ctx); err != nil {
		return err
	}
	s.diskGC.Start(ctx)
	s.closers = append(s.closers, stopCloser{s.diskGC})

	diskRC := controller.NewReconcileController(
		"disk", log, entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk), eas,
		controller.AdaptController(diskController), time.Minute, workers,
	)
	manager.AddController(diskRC)
	diskLeaseRC := controller.NewReconcileController(
		"disk-lease", log, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease), eas,
		controller.AdaptController(diskLeaseController), time.Minute, workers,
	)
	diskLeaseRC.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		if err := diskLeaseController.ReconcileOrphanLeases(ctx, disk.OrphanSweepGracePeriod); err != nil {
			log.Warn("periodic orphan lease sweep failed", "error", err)
		}
		return diskLeaseController.CleanupOldReleasedLeases(ctx)
	})
	manager.AddController(diskLeaseRC)

	diskWatch := disk.NewDiskWatchController(log, eas, diskLeaseRC)
	manager.AddController(controller.NewReconcileController(
		"disk-watch", log, entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk), eas,
		controller.AdaptController(diskWatch), time.Minute, 1,
	))
	volumeWatch := disk.NewDiskVolumeWatchController(log, eas, diskRC, s.nodeId())
	manager.AddController(controller.NewReconcileController(
		"disk-volume-watch", log, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskVolume), eas,
		controller.AdaptController(volumeWatch), 0, 1,
	))
	mountWatch := disk.NewDiskMountWatchController(log, eas, diskLeaseRC, s.nodeId())
	manager.AddController(controller.NewReconcileController(
		"disk-mount-watch", log, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskMount), eas,
		controller.AdaptController(mountWatch), 0, 1,
	))

	s.manager = manager
	return nil
}

func (s *NodeStorage) Close() error {
	var err error
	for _, closer := range s.closers {
		if closeErr := closer.Close(); closeErr != nil {
			err = multierror.Append(err, closeErr)
		}
	}
	return err
}

func (s *NodeStorage) SetRestartMode(v bool) {
	if s.dvc != nil {
		s.dvc.SetKeepMounts(v)
	}
	if s.dmc != nil {
		s.dmc.SetKeepMounts(v)
	}
}

// StorageAgent owns desired-state reconciliation for restored node storage.
type StorageAgent struct{ storage *NodeStorage }

// NewStorageAgent constructs reconciliation over restored node storage.
func NewStorageAgent(storage *NodeStorage) *StorageAgent { return &StorageAgent{storage: storage} }

func (a *StorageAgent) Start(ctx context.Context) error {
	if a.storage.manager == nil {
		return fmt.Errorf("node storage is not ready")
	}
	return a.storage.manager.Start(ctx)
}

func (a *StorageAgent) Close() error {
	if a.storage.manager != nil {
		a.storage.manager.Stop()
	}
	return nil
}
