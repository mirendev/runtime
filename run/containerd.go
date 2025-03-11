package run

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	"miren.dev/runtime/pkg/netdb"
)

type ContainerRunner struct {
	Log         *slog.Logger
	CC          *containerd.Client
	Namespace   string `asm:"namespace"`
	RunscBinary string `asm:"runsc_binary,optional"`
	Clickhouse  string `asm:"clickhouse-address,optional"`

	NetServ *network.ServiceManager
	Subnet  *netdb.Subnet

	DataPath string `asm:"data-path"`
	Tempdir  string `asm:"tempdir"`

	store *containerStore
}

func (c *ContainerRunner) Populated() error {
	if c.RunscBinary == "" {
		c.RunscBinary = "runsc-ignore"
	}

	if c.Clickhouse == "" {
		c.Clickhouse = "clickhouse:9000"
	}

	store, err := newContainerStore(filepath.Join(c.DataPath, "containers.db"))
	if err != nil {
		return fmt.Errorf("create container store: %w", err)
	}
	c.store = store

	return nil
}

// RestoreContainers restarts any containers that should be running
func (c *ContainerRunner) RestoreContainers(ctx context.Context) error {
	configs, err := c.store.List()
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	for _, cfg := range configs {
		c.Log.Info("checking container", "id", cfg.Id)

		// Check if container exists and is running
		container, err := c.CC.LoadContainer(ctx, cfg.Id)
		if err == nil {
			task, err := container.Task(ctx, nil)
			if err == nil {
				status, err := task.Status(ctx)
				if err == nil && status.Status == containerd.Running {
					c.Log.Info("container already running", "id", cfg.Id)
					continue
				}
			}

			// Container exists but task is not running properly
			_ = c.NukeContainer(ctx, cfg.Id)
		}

		if _, err := c.RunContainer(ctx, cfg); err != nil {
			c.Log.Error("failed to restore container", "id", cfg.Id, "error", err)
		}
	}

	return nil
}

type PortConfig struct {
	Port int
	Name string
	Type string
}

type MountConfig struct {
	Source string
	Target string
}

type ContainerConfig struct {
	Id        string
	App       string
	Image     string
	Version   string
	LogEntity string

	Labels map[string]string
	Env    map[string]string

	Privileged      bool
	SuperPrivileged bool

	Endpoint *network.EndpointConfig

	StaticDir string

	CGroupPath string

	Service string
	Command string

	Spec *oci.Spec

	Ports  []PortConfig
	Mounts []MountConfig

	AlwaysRun bool
}

func (c *ContainerConfig) DefaultHTTPApp() {
	c.Ports = append(c.Ports, PortConfig{
		Port: 3000,
		Name: "http",
		Type: "http",
	})
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

	config.Spec = spec

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

	if err := c.store.Save(config); err != nil {
		c.Log.Error("failed to persist container config", "id", config.Id, "error", err)
	}

	return config.Id, nil
}

func (c *ContainerRunner) writeResolve(path string, cfg *ContainerConfig) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(cfg.Endpoint.Bridge.Addresses) == 0 {
		return fmt.Errorf("no nameservers available in bridge config")
	}

	for _, addr := range cfg.Endpoint.Bridge.Addresses {
		if !addr.Addr().IsValid() {
			return fmt.Errorf("invalid nameserver address: %v", addr)
		}
		fmt.Fprintf(f, "nameserver %s\n", addr.Addr().String())
	}

	return nil
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
		"runtime.computer/app":     config.App,
		"runtime.computer/version": config.Version,
		"runtime.computer/ip":      config.Endpoint.Addresses[0].Addr().String(),
	}

	for _, p := range config.Ports {
		lbls[fmt.Sprintf("runtime.computer/endpoint:%s", p.Name)] = fmt.Sprintf("port=%d,type=%s", p.Port, p.Type)

		if p.Type == "http" {
			lbls["runtime.computer/http_host"] = fmt.Sprintf("%s:%d", config.Endpoint.Addresses[0].Addr().String(), p.Port)
		}
	}

	for k, v := range config.Labels {
		lbls[k] = v
	}

	if config.StaticDir != "" {
		lbls["runtime.computer/static_dir"] = config.StaticDir
	}

	tmpDir := filepath.Join(c.Tempdir, "containerd", config.Id)
	os.MkdirAll(tmpDir, 0755)

	resolvePath := filepath.Join(tmpDir, "resolv.conf")
	err = c.writeResolve(resolvePath, config)
	if err != nil {
		return nil, err
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
		{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      resolvePath,
			Options:     []string{"rbind", "rw"},
		},
	}

	for _, m := range config.Mounts {
		mounts = append(mounts, specs.Mount{
			Destination: m.Target,
			Type:        "bind",
			Source:      m.Source,
			Options:     []string{"rbind", "rw"},
		})
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
		oci.WithProcessCwd("/app"),
	}

	if config.Command != "" {
		c.Log.Debug("overriding command", "command", config.Command)
		specOpts = append(specOpts,
			oci.WithProcessArgs("/bin/sh", "-c", "exec "+config.Command))
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

	if config.SuperPrivileged {
		opts = append(opts,
			containerd.WithRuntime("io.containerd.runc.v2", nil),
		)
	} else {
		opts = append(opts,
			containerd.WithRuntime("io.containerd.runc.v2", &options.Options{
				BinaryName: c.RunscBinary,
			}),
		)
	}

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

	err = c.NetServ.SetupDNS(config.Endpoint.Bridge)
	if err != nil {
		return err
	}

	return task.Start(ctx)
}

func (c *ContainerRunner) RestartContainer(ctx context.Context, cfg *ContainerConfig) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	id := cfg.Id

	err := c.NukeContainer(ctx, id)
	if err != nil {
		c.Log.Debug("failed to nuke container before restart", "id", id, "error", err)
	}

	_, err = c.RunContainer(ctx, cfg)
	return err
}

func (c *ContainerRunner) StopContainer(ctx context.Context, id string) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, id)
	if err != nil {
		return err
	}

	labels, err := container.Labels(ctx)
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

	for l, v := range labels {
		if strings.HasPrefix(l, "runtime.computer/ip") {
			addr, err := netip.ParseAddr(v)
			if err == nil {
				err = c.Subnet.ReleaseAddr(addr)
				if err != nil {
					c.Log.Error("failed to release IP", "addr", addr, "err", err)
				}
			} else {
				c.Log.Error("failed to parse IP", "addr", v, "err", err)
			}

			c.Log.Debug("released IP", "addr", addr)
		}
	}

	// Ignore errors, as the directory might not exist if the container was
	// cleared up elsewhere.
	tmpDir := filepath.Join(c.Tempdir, "containerd", id)
	_ = os.RemoveAll(tmpDir)

	c.Log.Info("container stopped", "id", id)

	if err := c.store.Delete(id); err != nil {
		c.Log.Error("failed to remove container from store", "id", id, "error", err)
	}

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
