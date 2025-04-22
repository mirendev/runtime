package runner

import (
	"context"
	"log/slog"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	es "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/controllers/service"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/exec"
)

type RunnerConfig struct {
	Id            string `json:"id" cbor:"id" yaml:"id"`
	ServerAddress string `json:"server_address" cbor:"server_address" yaml:"server_address"`
	ListenAddress string `json:"listen_address" cbor:"listen_address" yaml:"listen_address"`
	Workers       int    `json:"workers" cbor:"workers" yaml:"workers"`
}

const (
	DefaulWorkers = 3
)

func NewRunner(log *slog.Logger, reg *asm.Registry, cfg RunnerConfig) *Runner {
	return &Runner{
		RunnerConfig: cfg,
		Log:          log,
		reg:          reg,
	}
}

type Runner struct {
	RunnerConfig

	Log *slog.Logger

	reg *asm.Registry

	cc *containerd.Client

	namespace string
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

	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBindAddr(r.ListenAddress))
	if err != nil {
		return err
	}

	client, err := rs.Connect(r.ServerAddress, "entities")
	if err != nil {
		return err
	}

	eas := &es.EntityAccessClient{Client: client}

	r.reg.Register("entity-client", eas)

	cm, err := r.SetupControllers(ctx, eas)
	if err != nil {
		return err
	}

	err = r.setupEntity(eas)
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

func (r *Runner) setupEntity(eas *es.EntityAccessClient) error {
	if r.Id == "" {
		return nil
	}

	node := compute_v1alpha.Node{
		Status:      compute_v1alpha.READY,
		Constraints: types.LabelSet("compute", "generic"),
		ApiAddress:  r.ListenAddress,
	}

	md := core_v1alpha.Metadata{
		Name:   r.Id,
		Labels: types.LabelSet("type", "dev"),
	}

	var aent es.Entity
	aent.SetAttrs(entity.Attrs(
		md.Encode,
		node.Encode,
		entity.Ident, "node/"+r.Id,
	))

	_, err := eas.Put(context.Background(), &aent)
	if err != nil {
		return err
	}

	return nil
}

func (r *Runner) SetupControllers(
	ctx context.Context,
	eas *es.EntityAccessClient,
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

	var serviceController service.ServiceController
	if err := r.reg.Populate(&serviceController); err != nil {
		return nil, err
	}

	var log *slog.Logger
	if err := r.reg.Resolve(&log); err != nil {
		return nil, err
	}

	err := sbc.Init(ctx)
	if err != nil {
		return nil, err
	}

	err = serviceController.Init(ctx)
	if err != nil {
		return nil, err
	}

	r.cc = sbc.CC
	r.namespace = sbc.Namespace

	workers := r.Workers
	if workers <= 0 {
		workers = DefaulWorkers
	}

	cm.AddController(
		controller.NewReconcileController(
			"sandbox",
			log,
			compute_v1alpha.Index(compute_v1alpha.KindSandbox, entity.Id("node/"+r.Id)),
			eas,
			controller.AdaptController(&sbc),
			time.Minute,
			workers,
		),
	)

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

	return cm, nil
}
