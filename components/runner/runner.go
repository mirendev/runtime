package runner

import (
	"context"
	"log/slog"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	es "miren.dev/runtime/api/entityserver/v1alpha"
	sb "miren.dev/runtime/api/sandbox/v1alpha"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
)

type RunnerConfig struct {
	Id            string `json:"id" cbor:"id" yaml:"id"`
	ServerAddress string `json:"server_address" cbor:"server_address" yaml:"server_address"`
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

	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		return err
	}

	client, err := rs.Connect(r.ServerAddress, "entities")
	if err != nil {
		return err
	}

	eas := es.EntityAccessClient{Client: client}

	cm, err := r.SetupControllers(ctx, &eas)
	if err != nil {
		return err
	}

	err = r.setupEntity(&eas)
	if err != nil {
		return err
	}

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

	node := sb.Node{
		Status:      sb.READY,
		Constraints: types.LabelSet("compute", "generic"),
	}

	var aent es.Entity
	aent.SetAttrs(entity.Attrs(
		node.Encode,
		entity.Ident, r.Id,
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

	var sbc sandbox.SandboxController
	if err := r.reg.Populate(&sbc); err != nil {
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
			sb.Index(sb.KindSandbox, entity.Id(r.Id)),
			eas,
			controller.AdaptController(&sbc),
			time.Minute,
			workers,
		),
	)

	return cm, nil
}
