package run

import (
	"context"
	"io"
	"log/slog"
	"net/netip"
	"os/exec"

	"github.com/containerd/containerd/api/types/runc/options"
	containerd "github.com/containerd/containerd/v2/client"
	tarchive "github.com/containerd/containerd/v2/core/transfer/archive"
	"github.com/containerd/containerd/v2/core/transfer/image"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/platforms"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
)

type ImageImporter struct {
	CC        *containerd.Client
	Namespace string `asm:"namespace"`
}

func (i *ImageImporter) ImportImage(ctx context.Context, r io.Reader, indexName string) error {
	ctx = namespaces.WithNamespace(ctx, i.Namespace)
	var opts []image.StoreOpt
	opts = append(opts, image.WithNamedPrefix("mn-tmp", true))

	// Only when all-platforms not specified, we will check platform value
	// Implicitly if the platforms is empty, it means all-platforms
	platSpec := platforms.DefaultSpec()
	opts = append(opts, image.WithPlatforms(platSpec))

	opts = append(opts, image.WithUnpack(platSpec, ""))

	is := image.NewStore(indexName, opts...)

	var iopts []tarchive.ImportOpt

	iis := tarchive.NewImageImportStream(r, "", iopts...)

	return i.CC.Transfer(ctx, iis, is)
}

type ContainerRunner struct {
	Log       *slog.Logger
	CC        *containerd.Client
	Namespace string `asm:"namespace"`
}

type ContainerConfig struct {
	Id     string
	App    string
	Image  string
	IPs    []netip.Prefix
	Subnet *Subnet

	StaticDir string
}

func (c *ContainerRunner) RunContainer(ctx context.Context, config *ContainerConfig) (string, error) {
	if config.Id == "" {
		config.Id = identity.NewID()
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
		"app":       config.App,
		"http_host": config.IPs[0].Addr().String() + ":3000",
	}

	if config.StaticDir != "" {
		lbls["static_dir"] = config.StaticDir
	}

	opts = append(opts,
		containerd.WithNewSnapshot(config.Id, img),
		containerd.WithNewSpec(
			oci.WithImageConfig(img),
			oci.WithEnv([]string{"PORT=3000"}),
			//oci.WithMounts(mounts),
		),
		containerd.WithRuntime("io.containerd.runc.v2", &options.Options{
			BinaryName: "runsc-ignore",
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
			"-d": "clickhouse:9000",
			"-e": config.Id,
		}))
	if err != nil {
		return err
	}

	err = setupNetwork(c.Log, config.Subnet, task, config)
	if err != nil {
		return err
	}

	return task.Start(ctx)
}
