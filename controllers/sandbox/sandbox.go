package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"gvisor.dev/gvisor/pkg/shim/v1/runtimeoptions"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/netdb"

	"miren.dev/runtime/api/sandbox/v1alpha"
)

const sandboxImage = "registry.k8s.io/pause:3.8"

type SandboxController struct {
	Log *slog.Logger
	CC  *containerd.Client

	Namespace   string `asm:"namespace"`
	RunscBinary string `asm:"runsc_binary,optional"`

	NetServ *network.ServiceManager

	Bridge string `asm:"bridge-iface"`
	Subnet *netdb.Subnet

	DataPath string `asm:"data-path"`
	Tempdir  string `asm:"tempdir"`

	LogsMaintainer   *observability.LogsMaintainer
	ResourcesMonitor *observability.ResourcesMonitor

	RunscMon *observability.RunSCMonitor

	topCtx context.Context
	cancel func()

	mu       sync.Mutex
	monitors int
	cond     *sync.Cond

	runscConfigPath string
}

func (c *SandboxController) setupRunscConfig() error {
	if c.RunscBinary == "" {
		c.RunscBinary = "runsc"
	}

	path := filepath.Join(c.Tempdir, "runsc.toml")

	exe, err := exec.LookPath(c.RunscBinary)
	if err != nil {
		return fmt.Errorf("failed to find runsc binary: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create runsc config: %w", err)
	}

	defer f.Close()

	fmt.Fprintf(f, "binary_name = \"%s\"\n", exe)

	c.runscConfigPath = path

	return nil
}

func SetupRunsc(dir string) (string, string) {

	path := filepath.Join(dir, "runsc-entry")
	pic := filepath.Join(dir, "pod-init-config.json")

	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(f,
		"#!/bin/bash\nexec runsc -pod-init-config \"%s\" \"$@\"\n", pic)

	defer f.Close()

	err = os.Chmod(path, 0755)
	if err != nil {
		panic(err)
	}

	return path, pic
}

func (c *SandboxController) Init(ctx context.Context) error {
	runscBin, podInit := SetupRunsc(c.Tempdir)
	c.RunscBinary = runscBin

	c.RunscMon.SetEndpoint(filepath.Join(c.Tempdir, "runsc-mon.sock"))

	err := c.RunscMon.WritePodInit(podInit)
	if err != nil {
		return fmt.Errorf("failed to write runsc config: %w", err)
	}

	err = c.RunscMon.Monitor(ctx)
	if err != nil {
		return fmt.Errorf("failed to start runsc monitor: %w", err)
	}

	err = c.setupRunscConfig()
	if err != nil {
		return err
	}

	err = c.LogsMaintainer.Setup(ctx)
	if err != nil {
		return err
	}

	err = c.ResourcesMonitor.Setup(ctx)
	if err != nil {
		return err
	}

	c.topCtx, c.cancel = context.WithCancel(ctx)

	c.cond = sync.NewCond(&c.mu)

	return nil
}

func (c *SandboxController) exitMonitor() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.monitors--
	c.cond.Broadcast()
}

func (c *SandboxController) enterMonitor() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.monitors++
}

func (c *SandboxController) Close() error {
	c.cancel()

	c.mu.Lock()
	for c.monitors > 0 {
		c.cond.Wait()
	}
	c.mu.Unlock()

	err := c.RunscMon.Close()
	if err != nil {
		c.Log.Error("failed to close runsc monitor", "err", err)
	}

	return nil
}

func (c *SandboxController) Create(ctx context.Context, co *v1alpha.Sandbox) error {
	c.Log.Debug("creating container", "id", co.ID)

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	ep, err := c.allocateNetwork(ctx, co)
	if err != nil {
		return fmt.Errorf("failed to allocate network: %w", err)
	}

	opts, err := c.buildSpec(ctx, co, ep)
	if err != nil {
		return fmt.Errorf("failed to build container spec: %w", err)
	}

	container, err := c.CC.NewContainer(ctx, co.ID, opts...)
	if err != nil {
		return errors.Wrapf(err, "failed to create container %s", co.ID)
	}

	defer func() {
		if err != nil {
			task, _ := container.Task(ctx, nil)
			if task != nil {
				task.Delete(ctx, containerd.WithProcessKill)
			}

			derr := container.Delete(ctx, containerd.WithSnapshotCleanup)
			if derr != nil {
				c.Log.Error("failed to cleanup container", "id", co.ID, "err", derr)
			}
		}
	}()

	task, err := c.bootInitialTask(ctx, co, ep, container)
	if err != nil {
		return err
	}

	cgroupPath, err := observability.CGroupPathForPid(task.Pid())
	if err != nil {
		c.Log.Error("failed to get cgroup path", "pid", task.Pid(), "err", err)
	} else {
		c.enterMonitor()

		go func() {
			defer c.exitMonitor()
			err := c.ResourcesMonitor.Monitor(c.topCtx, co.ID, cgroupPath)
			if err != nil {
				c.Log.Error("failed to monitor container resources", "id", co.ID, "err", err)
			}
		}()
	}

	c.Log.Info("sanbox started", "id", co.ID, "namespace", c.Namespace)

	err = c.bootContainers(ctx, co, ep, int(task.Pid()))
	if err != nil {
		return err
	}

	return nil
}

func (c *SandboxController) Delete(ctx context.Context, entity *entity.Entity) error {
	id := entity.ID

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

	return nil
}

func (c *SandboxController) allocateNetwork(
	ctx context.Context,
	co *v1alpha.Sandbox,
) (*network.EndpointConfig, error) {
	if c.Bridge == "" {
		return nil, fmt.Errorf("bridge name not configured")
	}

	if c.Subnet == nil {
		return nil, fmt.Errorf("subnet not configured")
	}

	var (
		ep  *network.EndpointConfig
		err error
	)

	if len(co.Network) > 0 {
		var prefixes []netip.Prefix

		for _, net := range co.Network {
			prefix, err := netip.ParsePrefix(net.Address)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %s", net.Address)
			}

			prefixes = append(prefixes, prefix)
		}

		ep, err = network.SetupOnBridge(c.Bridge, c.Subnet, prefixes)
		if err != nil {
			return nil, err
		}

	} else {
		ep, err = network.AllocateOnBridge(c.Bridge, c.Subnet)
		if err != nil {
			return nil, err
		}

		co.Network = append(co.Network, v1alpha.Network{
			Address: ep.Addresses[0].String(),
			Subnet:  c.Bridge,
		})
	}

	c.Log.Debug("allocated network endpoint", "bridge", c.Bridge, "addresses", ep.Addresses)

	return ep, nil
}

func (c *SandboxController) buildSpec(
	ctx context.Context,
	co *v1alpha.Sandbox,
	ep *network.EndpointConfig,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, sandboxImage)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, sandboxImage, containerd.WithPullUnpack)
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", sandboxImage, err)
		}

		img, err = c.CC.GetImage(ctx, sandboxImage)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", sandboxImage, err)
		}
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	lbls := map[string]string{}

	for _, lbl := range co.Label {
		if key, val, ok := strings.Cut(lbl, "="); ok {
			lbls[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}

	//if config.StaticDir != "" {
	//lbls["runtime.computer/static_dir"] = config.StaticDir
	//}

	id := co.ID

	tmpDir := filepath.Join(c.Tempdir, "containerd", id)
	os.MkdirAll(tmpDir, 0755)

	resolvePath := c.sandboxPath(co, "resolv.conf")
	err = c.writeResolve(resolvePath, ep)
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

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithProcessCwd("/"),
		oci.WithAnnotations(map[string]string{
			"io.kubernetes.cri.container-type": "sandbox",
		}),
	}

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runsc.v1", &runtimeoptions.Options{
			TypeUrl:    "io.containerd.runsc.v1.options",
			ConfigPath: c.runscConfigPath,
		}),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

func (c *SandboxController) writeResolve(path string, ep *network.EndpointConfig) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(ep.Bridge.Addresses) == 0 {
		return fmt.Errorf("no nameservers available in bridge config")
	}

	for _, addr := range ep.Bridge.Addresses {
		if !addr.Addr().IsValid() {
			return fmt.Errorf("invalid nameserver address: %v", addr)
		}
		fmt.Fprintf(f, "nameserver %s\n", addr.Addr().String())
	}

	return nil
}

func (c *SandboxController) bootInitialTask(
	ctx context.Context,
	co *v1alpha.Sandbox,
	ep *network.EndpointConfig,
	container containerd.Container,
) (containerd.Task, error) {
	c.Log.Info("booting sandbox task")

	task, err := container.NewTask(ctx, cio.NullIO)
	if err != nil {
		return nil, err
	}

	err = network.ConfigureNetNS(c.Log, int(task.Pid()), ep)
	if err != nil {
		return nil, err
	}

	err = c.NetServ.SetupDNS(ep.Bridge)
	if err != nil {
		return nil, err
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, err
	}

	return task, nil
}

func (c *SandboxController) bootContainers(
	ctx context.Context,
	sb *v1alpha.Sandbox,
	ep *network.EndpointConfig,
	sbPid int,
) error {
	c.Log.Info("booting containers")

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	for _, container := range sb.Container {
		opts, err := c.buildSubContainerSpec(ctx, sb, &container, ep, sbPid)
		if err != nil {
			return fmt.Errorf("failed to build container spec: %w", err)
		}

		id := fmt.Sprintf("%s-%s", sb.ID, container.Name)
		container, err := c.CC.NewContainer(ctx, id, opts...)
		if err != nil {
			return errors.Wrapf(err, "failed to create container %s", sb.ID)
		}

		task, err := container.NewTask(ctx, cio.NullIO)
		if err != nil {
			return err
		}

		err = task.Start(ctx)
		if err != nil {
			return err
		}

		c.Log.Info("container started", "id", container.ID())
	}

	return nil
}

func (c *SandboxController) sandboxPath(sb *v1alpha.Sandbox, sub string) string {
	return filepath.Join(c.Tempdir, "containerd", sb.ID, sub)
}

func (c *SandboxController) buildSubContainerSpec(
	ctx context.Context,
	sb *v1alpha.Sandbox,
	co *v1alpha.Container,
	ep *network.EndpointConfig,
	sbPid int,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, sandboxImage)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, sandboxImage, containerd.WithPullUnpack)
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", sandboxImage, err)
		}

		img, err = c.CC.GetImage(ctx, sandboxImage)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", sandboxImage, err)
		}
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	lbls := map[string]string{}

	id := fmt.Sprintf("%s-%s", sb.ID, co.Name)

	resolvePath := c.sandboxPath(sb, "resolv.conf")

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

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithProcessCwd("/"),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/net", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.IPCNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/ipc", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.TimeNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/time", sbPid),
		}),
		oci.WithAnnotations(map[string]string{
			"io.kubernetes.cri.container-type": "container",
			"io.kubernetes.cri.sandbox-id":     sb.ID,
		}),
	}

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runsc.v1", &runtimeoptions.Options{
			TypeUrl:    "io.containerd.runsc.v1.options",
			ConfigPath: c.runscConfigPath,
		}),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}
