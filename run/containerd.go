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
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/idgen"
)

type ContainerRunner struct {
	Log         *slog.Logger
	CC          *containerd.Client
	Namespace   string `asm:"namespace"`
	RunscBinary string `asm:"runsc_binary,optional"`
	Clickhouse  string `asm:"clickhouse-address,optional"`
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
	Id        string
	App       string
	Image     string
	Version   string
	LogEntity string

	Labels map[string]string
	Env    map[string]string

	Privileged bool

	Endpoint *network.EndpointConfig

	StaticDir string

	CGroupPath string
}

func (c *ContainerRunner) RunContainer(ctx context.Context, config *ContainerConfig) (string, error) {
	if config.Id == "" {
		config.Id = idgen.Gen("c")
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
		"miren.dev/version":       config.Version,
		"miren.dev/http_host":     config.Endpoint.Addresses[0].Addr().String() + ":3000",
		"miren.dev/ip":            config.Endpoint.Addresses[0].Addr().String(),
		"miren.dev/endpoint:http": "port=3000,type=http",
	}

	for k, v := range config.Labels {
		lbls[k] = v
	}

	if config.StaticDir != "" {
		lbls["miren.dev/static_dir"] = config.StaticDir
	}

	mounts := []specs.Mount{
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
	}

	var envs []string

	for k, v := range config.Env {
		envs = append(envs, k+"="+v)
	}

	envs = append(envs, "PORT=3000")

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithEnv(envs),
		oci.WithHostResolvconf,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
	}

	if config.Privileged {
		specOpts = append(specOpts,
			oci.WithPrivileged,
			oci.WithAllDevicesAllowed,
			oci.WithWriteableCgroupfs,
			oci.WithAddedCapabilities([]string{"CAP_SYS_ADMIN"}),
		)
	}

	opts = append(opts,
		containerd.WithNewSnapshot(config.Id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runc.v2", &options.Options{
			BinaryName: c.RunscBinary,
		}),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

var perms = []string{
	"CAP_CHOWN",
	"CAP_DAC_OVERRIDE",
	"CAP_DAC_READ_SEARCH",
	"CAP_FOWNER",
	"CAP_FSETID",
	"CAP_KILL",
	"CAP_SETGID",
	"CAP_SETUID",
	"CAP_SETPCAP",
	"CAP_LINUX_IMMUTABLE",
	"CAP_NET_BIND_SERVICE",
	"CAP_NET_BROADCAST",
	"CAP_NET_ADMIN",
	"CAP_NET_RAW",
	"CAP_IPC_LOCK",
	"CAP_IPC_OWNER",
	"CAP_SYS_MODULE",
	"CAP_SYS_RAWIO",
	"CAP_SYS_CHROOT",
	"CAP_SYS_PTRACE",
	"CAP_SYS_PACCT",
	"CAP_SYS_ADMIN",
	"CAP_SYS_BOOT",
	"CAP_SYS_NICE",
	"CAP_SYS_RESOURCE",
	"CAP_SYS_TIME",
	"CAP_SYS_TTY_CONFIG",
	"CAP_MKNOD",
	"CAP_LEASE",
	"CAP_AUDIT_WRITE",
	"CAP_AUDIT_CONTROL",
	"CAP_SETFCAP",
	"CAP_MAC_OVERRIDE",
	"CAP_MAC_ADMIN",
	"CAP_SYSLOG",
	"CAP_WAKE_ALARM",
	"CAP_BLOCK_SUSPEND",
	"CAP_AUDIT_READ",
	"CAP_PERFMON",
	"CAP_BPF",
	"CAP_CHECKPOINT_RESTORE",
}

func (c *ContainerRunner) bootInitialTask(ctx context.Context, config *ContainerConfig, container containerd.Container) error {
	c.Log.Info("booting task")

	exe, err := exec.LookPath("containerd-log-ingress")
	if err != nil {
		return err
	}

	id := config.LogEntity
	if id == "" {
		id = config.Id
	}

	task, err := container.NewTask(ctx,
		cio.BinaryIO(exe, map[string]string{
			"-d": c.Clickhouse,
			"-e": id,
		}))
	if err != nil {
		return err
	}

	err = network.ConfigureNetNS(c.Log, int(task.Pid()), config.Endpoint)
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

// NukeContainer stops and deletes a container.
// It doesn't return an error if the container is missing, as that is the desired state.
func (c *ContainerRunner) NukeContainer(ctx context.Context, id string) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, id)
	if err != nil {
		return nil
	}

	task, err := container.Task(ctx, nil)
	if err == nil {
		task.Delete(ctx, containerd.WithProcessKill)
		task.Delete(ctx)
	}

	err = container.Delete(ctx, containerd.WithSnapshotCleanup)
	if err != nil {
		return err
	}

	c.Log.Info("container stopped", "id", id)

	return nil
}
