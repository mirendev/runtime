package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	es "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/controllers/disk"
	"miren.dev/runtime/controllers/ingress"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/controllers/service"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/multierror"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/exec"
)

type RunnerConfig struct {
	Id            string `json:"id" cbor:"id" yaml:"id"`
	ListenAddress string `json:"listen_address" cbor:"listen_address" yaml:"listen_address"`
	Workers       int    `json:"workers" cbor:"workers" yaml:"workers"`
	DataPath      string `json:"data_path" cbor:"data_path" yaml:"data_path" asm:"data-path"`

	// Optional RPC configuration for advanced setups
	// If not provided, a default insecure connection will be used
	// to connect to the server address.
	Config *clientconfig.Config `json:"config" cbor:"config" yaml:"config"`

	// Optional cloud authentication configuration for disk replication
	CloudAuth *coordinate.CloudAuthConfig `json:"cloud_auth,omitempty" cbor:"cloud_auth,omitempty" yaml:"cloud_auth,omitempty"`
}

const (
	DefaulWorkers = 3
)

func NewRunner(log *slog.Logger, reg *asm.Registry, cfg RunnerConfig) (*Runner, error) {
	if cfg.DataPath == "" {
		return nil, fmt.Errorf("data_path is required")
	}

	if cfg.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	return &Runner{
		RunnerConfig: cfg,
		Log:          log.With("module", "runner"),
		reg:          reg,
	}, nil
}

type Runner struct {
	RunnerConfig

	Log *slog.Logger

	reg *asm.Registry

	cc *containerd.Client

	ec *entityserver.Client
	se *entityserver.Session

	closers []io.Closer

	namespace string

	sbController *sandbox.SandboxController
}

func (r *Runner) Close() error {
	var err error

	for _, c := range r.closers {
		xerr := c.Close()
		if xerr != nil {
			err = multierror.Append(err, xerr)
		}
	}

	return err
}

// Drain sets the runner's node status to disabled and stops all running sandboxes
func (r *Runner) Drain(ctx context.Context) error {
	if r.ec == nil || r.Id == "" {
		return fmt.Errorf("runner not initialized with entity client")
	}

	r.Log.Info("draining runner", "id", r.Id)

	// Set node status to disabled
	r.Log.Info("setting node status to disabled", "id", r.Id)
	err := r.ec.UpdateAttrs(ctx, entity.Id(r.Id), (&compute_v1alpha.Node{
		Status: compute_v1alpha.DISABLED,
	}).Encode)
	if err != nil {
		return fmt.Errorf("failed to set node status to disabled: %w", err)
	}

	r.Log.Info("node status set to disabled", "id", r.Id)

	// List all sandboxes scheduled to this node
	idx := compute_v1alpha.Index(compute_v1alpha.KindSandbox, entity.Id("node/"+r.Id))
	results, err := r.ec.List(ctx, idx)
	if err != nil {
		return fmt.Errorf("failed to query sandboxes on node: %w", err)
	}

	sandboxCount := results.Length()
	r.Log.Info("found sandboxes to drain", "count", sandboxCount, "node", r.Id)

	// Stop each sandbox
	var drainErr error
	stoppedCount := 0
	for results.Next() {
		md := results.Metadata()
		if md == nil {
			continue
		}

		r.Log.Info("stopping sandbox", "id", md.ID)
		err := r.sbController.Delete(ctx, md.ID)
		if err != nil {
			r.Log.Error("failed to stop sandbox", "id", md.ID, "error", err)
			drainErr = multierror.Append(drainErr, fmt.Errorf("failed to stop sandbox %s: %w", md.ID, err))
		} else {
			r.Log.Info("stopped sandbox", "id", md.ID)
			stoppedCount++
		}
	}

	if drainErr != nil {
		return fmt.Errorf("errors during drain: %w", drainErr)
	}

	r.Log.Info("runner drained successfully", "id", r.Id, "sandboxes_stopped", stoppedCount)
	return nil
}

func (r *Runner) ContainerdNamespace() string {
	return r.namespace
}

func (r *Runner) ContainerdContainerForSandbox(ctx context.Context, id entity.Id) (containerd.Container, error) {
	cl, err := r.cc.ContainerService().List(ctx)
	if err != nil {
		return nil, err
	}

	for _, c := range cl {
		if c.Labels["runtime.computer/entity-id"] == string(id) {
			return r.cc.LoadContainer(ctx, c.ID)
		}
	}

	return nil, nil
}

func (r *Runner) Start(ctx context.Context) error {
	r.Log.Info("Starting runner", "id", r.Id)

	var (
		rs     *rpc.State
		err    error
		client *rpc.NetworkClient
	)

	if r.Config == nil {
		rs, err = rpc.NewState(ctx, rpc.WithLogger(r.Log), rpc.WithBindAddr(r.ListenAddress), rpc.WithSkipVerify)
		if err != nil {
			return err
		}

		client, err = rs.Connect("", "entities")
		if err != nil {
			return err
		}
	} else {
		rs, err = r.Config.State(ctx, rpc.WithLogger(r.Log), rpc.WithBindAddr(r.ListenAddress))
		if err != nil {
			return err
		}

		client, err = rs.Client("entities")
		if err != nil {
			return err
		}
	}

	eas := es.NewEntityAccessClient(client)

	ec := entityserver.NewClient(r.Log, eas)

	cm, err := r.SetupControllers(ctx, eas, rs.Server())
	if err != nil {
		return err
	}

	err = r.setupEntity(ctx, ec)
	if err != nil {
		return err
	}

	var es exec.Server

	if err := r.reg.Populate(&es); err != nil {
		return err
	}

	rs.Server().ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(&es))

	r.Log.Info("Registered exec server")

	err = cm.Start(ctx)
	if err != nil {
		return err
	}

	r.Log.Info("Runner running", "id", r.Id)

	return nil
}

func (r *Runner) setupEntity(ctx context.Context, ec *entityserver.Client) error {
	if r.Id == "" {
		return nil
	}

	sess, ec, err := ec.NewSession(ctx, "runner health")
	if err != nil {
		return err
	}

	r.ec = ec
	r.se = sess

	node := compute_v1alpha.Node{
		Constraints: types.LabelSet("compute", "generic"),
		ApiAddress:  r.ListenAddress,
	}

	res, err := ec.CreateOrUpdate(ctx, r.Id, &node)
	if err != nil {
		return err
	}

	err = ec.UpdateAttrs(ctx, res, (&compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	}).Encode)
	if err != nil {
		return err
	}

	r.Log.Info("Registered runner", "id", res)

	return nil
}

func (r *Runner) SetupControllers(
	ctx context.Context,
	eas *es.EntityAccessClient,
	rs *rpc.Server,
) (
	*controller.ControllerManager,
	error,
) {
	cm := controller.NewControllerManager()

	r.reg.Register("entity-client", eas)

	var sbc sandbox.SandboxController
	if err := r.reg.Populate(&sbc); err != nil {
		return nil, err
	}
	sbc.Log = sbc.Log.With("module", "sandbox", "runner_id", r.Id)

	r.closers = append(r.closers, &sbc)

	rs.ExposeValue("dev.miren.runtime/sandbox.metrics", metric_v1alpha.AdaptSandboxMetrics(sbc.Metrics))

	var serviceController service.ServiceController
	if err := r.reg.Populate(&serviceController); err != nil {
		return nil, err
	}

	var log *slog.Logger
	if err := r.reg.Resolve(&log); err != nil {
		return nil, err
	}

	defaultRouteAppController := ingress.NewDefaultRouteAppController(log, eas)
	defaultRouteController := ingress.NewDefaultRouteController(log, eas)

	// Initialize disk controllers
	// Create LSVD client for disk operations
	dataPath := filepath.Join(r.DataPath, "disk-data")
	err := os.MkdirAll(dataPath, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk data path: %w", err)
	}

	// Create LSVD client with optional replication support
	var lsvdClient disk.LsvdClient
	var diskController *disk.DiskController
	var diskLeaseController *disk.DiskLeaseController

	if r.CloudAuth != nil && r.CloudAuth.Enabled {
		log.Info("Creating LSVD client with cloud replication",
			"cloud_url", r.CloudAuth.CloudURL)

		// Create auth client from cloud auth config
		keyPair, err := cloudauth.LoadKeyPairFromPEM(r.CloudAuth.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load keypair from private key: %w", err)
		}

		authClient, err := cloudauth.NewAuthClient(r.CloudAuth.CloudURL, keyPair)
		if err != nil {
			return nil, fmt.Errorf("failed to create auth client: %w", err)
		}

		// Create both local+replica and remote-only clients to support both modes
		localReplicaClient := disk.NewLsvdClient(log, dataPath, disk.WithReplica(authClient, r.CloudAuth.CloudURL))
		remoteOnlyClient := disk.NewLsvdClient(log, dataPath, disk.WithRemoteOnly(authClient, r.CloudAuth.CloudURL))

		lsvdClient = localReplicaClient
		diskController = disk.NewDiskControllerWithClients(log, eas, lsvdClient, localReplicaClient, remoteOnlyClient)
		diskLeaseController = disk.NewDiskLeaseControllerWithClients(log, eas, lsvdClient, localReplicaClient, remoteOnlyClient)
	} else {
		lsvdClient = disk.NewLsvdClient(log, dataPath)
		diskController = disk.NewDiskController(log, eas, lsvdClient)
		diskLeaseController = disk.NewDiskLeaseController(log, eas, lsvdClient)
	}

	// Add disk controller to closers list so it gets cleaned up on shutdown
	r.closers = append(r.closers, diskController)

	err = sbc.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = serviceController.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = diskController.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = diskLeaseController.Init(ctx)
	if err != nil {
		return nil, err
	}

	r.cc = sbc.CC
	r.namespace = sbc.Namespace
	r.sbController = &sbc

	workers := r.Workers
	if workers <= 0 {
		workers = DefaulWorkers
	}

	sbController := controller.NewReconcileController(
		"sandbox",
		log,
		compute_v1alpha.Index(compute_v1alpha.KindSandbox, entity.Id("node/"+r.Id)),
		eas,
		controller.AdaptController(&sbc),
		time.Minute,
		workers,
	)

	sbController.SetPeriodic(5*time.Minute, sbc.Periodic)

	cm.AddController(sbController)

	cm.AddController(
		controller.NewReconcileController(
			"service",
			log,
			entity.Ref(entity.EntityKind, network_v1alpha.KindService),
			eas,
			controller.AdaptController(&serviceController),
			time.Minute,
			workers,
		),
	)

	cm.AddController(
		controller.NewReconcileController(
			"endpoints",
			log,
			entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints),
			eas,
			serviceController.UpdateEndpoints,
			0,
			workers,
		),
	)

	cm.AddController(
		controller.NewReconcileController(
			"default-route-app",
			log,
			entity.Ref(entity.EntityKind, core_v1alpha.KindApp),
			eas,
			controller.AdaptController(defaultRouteAppController),
			0, // No periodic resync needed
			1, // Single worker is sufficient for this controller
		),
	)

	cm.AddController(
		controller.NewReconcileController(
			"default-route",
			log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute),
			eas,
			controller.AdaptController(defaultRouteController),
			0, // No periodic resync needed
			1, // Single worker is sufficient for this controller
		),
	)

	// Add disk controller
	cm.AddController(
		controller.NewReconcileController(
			"disk",
			log,
			entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk),
			eas,
			controller.AdaptController(diskController),
			time.Minute,
			workers,
		),
	)

	// Add disk lease controller
	diskLeaseRC := controller.NewReconcileController(
		"disk-lease",
		log,
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease),
		eas,
		controller.AdaptController(diskLeaseController),
		time.Minute,
		workers,
	)

	// Set up periodic cleanup of old released leases (every 5 minutes)
	diskLeaseRC.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		return diskLeaseController.CleanupOldReleasedLeases(ctx)
	})

	cm.AddController(diskLeaseRC)

	return cm, nil
}
