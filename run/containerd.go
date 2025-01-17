package run

import (
	"context"
	"log/slog"
	"os/exec"

	"github.com/containerd/containerd/api/types/runc/options"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
	"miren.dev/runtime/network"
)

type ContainerRunner struct {
	Log         *slog.Logger
	CC          *containerd.Client
	Namespace   string `asm:"namespace"`
	RunscBinary string `asm:"runsc_binary,optional"`
	Clickhouse  string `asm:"clickhouse_address,optional"`
}

func (c *ContainerRunner) Populated() error {
	if c.RunscBinary == "" {
		c.RunscBinary = "runsc-ignore"
	}

	if c.Clickhouse == "" {
		c.Clickhouse = "clickhouse:9000"
	}

	return nil
}

type ContainerConfig struct {
	Id    string
	App   string
	Image string

	Endpoint *network.EndpointConfig

	StaticDir string

	CGroupPath string
}

func (c *ContainerRunner) RunContainer(ctx context.Context, config *ContainerConfig) (string, error) {
	if config.Id == "" {
		config.Id = identity.NewID()
	}

	if config.Endpoint == nil {
		return "", errors.New("network endpoint config is required")
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	opts, err := c.buildSpec(ctx, config)
	if err != nil {
		return "", err
	}

	container, err := c.CC.NewContainer(ctx, config.Id, opts...)
	if err != nil {
		return "", errors.Wrapf(err, "failed to create container %s", config.Id)
	}

	spec, err := container.Spec(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get container spec %s", config.Id)
	}

	config.CGroupPath = spec.Linux.CgroupsPath

	err = c.bootInitialTask(ctx, config, container)
	if err != nil {
		task, _ := container.Task(ctx, nil)
		if task != nil {
			task.Delete(ctx, containerd.WithProcessKill)
		}

		derr := container.Delete(ctx, containerd.WithSnapshotCleanup)
		if derr != nil {
			c.Log.Error("failed to cleanup container", "id", config.Id, "err", derr)
		}
		return "", err
	}

	c.Log.Info("container started", "id", config.Id, "namespace", c.Namespace)

	return config.Id, nil
}

func (c *ContainerRunner) buildSpec(ctx context.Context, config *ContainerConfig) ([]containerd.NewContainerOpts, error) {
	img, err := c.CC.GetImage(ctx, config.Image)
	if err != nil {
		return nil, err
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	lbls := map[string]string{
		"miren.dev/app":           config.App,
		"miren.dev/http_host":     config.Endpoint.Addresses[0].Addr().String() + ":3000",
		"miren.dev/ip":            config.Endpoint.Addresses[0].Addr().String(),
		"miren.dev/endpoint:http": "port=3000,type=http",
	}

	if config.StaticDir != "" {
		lbls["miren.dev/static_dir"] = config.StaticDir
	}

	opts = append(opts,
		containerd.WithNewSnapshot(config.Id, img),
		containerd.WithNewSpec(
			oci.WithImageConfig(img),
			oci.WithEnv([]string{"PORT=3000"}),
			//oci.WithMounts(mounts),
		),
		containerd.WithRuntime("io.containerd.runc.v2", &options.Options{
			BinaryName: c.RunscBinary,
		}),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

func (c *ContainerRunner) bootInitialTask(ctx context.Context, config *ContainerConfig, container containerd.Container) error {
	c.Log.Info("booting task")

	exe, err := exec.LookPath("containerd-log-ingress")
	if err != nil {
		return err
	}

	task, err := container.NewTask(ctx,
		cio.BinaryIO(exe, map[string]string{
			"-d": c.Clickhouse,
			"-e": config.Id,
		}))
	if err != nil {
		return err
	}

	err = setupNetwork(c.Log, task, config)
	if err != nil {
		return err
	}

	return task.Start(ctx)
}

func (c *ContainerRunner) StopContainer(ctx context.Context, id string) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, id)
	if err != nil {
		return err
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		return err
	}

	if task != nil {
		_, err = task.Delete(ctx, containerd.WithProcessKill)
		if err != nil {
			return err
		}
	}

	err = container.Delete(ctx, containerd.WithSnapshotCleanup)
	if err != nil {
		return err
	}

	c.Log.Info("container stopped", "id", id)

	return nil
}
