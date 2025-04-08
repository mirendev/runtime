package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pelletier/go-toml/v2"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/shim/v1/runtimeoptions"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/netdb"

	"miren.dev/runtime/api/sandbox/v1alpha"
)

const (
	// defaultSandboxOOMAdj is default omm adj for sandbox container. (kubernetes#47938).
	defaultSandboxOOMAdj = -998
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

	running sync.WaitGroup
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

func (c *SandboxController) setupNewRunscConfig(path string, opts map[string]string) error {
	if c.RunscBinary == "" {
		c.RunscBinary = "runsc"
	}

	exe, err := exec.LookPath(c.RunscBinary)
	if err != nil {
		return fmt.Errorf("failed to find runsc binary: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create runsc config: %w", err)
	}

	defer f.Close()

	top := map[string]any{
		"binary_name": exe,
	}

	if len(opts) > 0 {
		top["runsc_config"] = opts
	}

	return toml.NewEncoder(f).Encode(top)
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

	_, err = network.SetupBridge(&network.BridgeConfig{
		Name:      c.Bridge,
		Addresses: []netip.Prefix{c.Subnet.Router()},
	})
	if err != nil {
		return err
	}

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

	c.running.Wait()

	return nil
}

const (
	sandboxVersionLabel = "runtime.computer/sandbox-version"
	sandboxEntityLabel  = "runtime.computer/entity-id"
)

const (
	notFound = iota
	same
	differentVersion
)

// canUpdateInPlace checks if the sandbox can be updated in place without destroying it.
func (c *SandboxController) canUpdateInPlace(ctx context.Context, sb *v1alpha.Sandbox, meta *entity.Meta) (bool, *v1alpha.Sandbox, error) {
	// We support the ability to update a subnet of elements of the sandbox while running.
	// For everything else, we destroy it and rebuild it fully with Create.

	oldMeta, err := c.readEntity(ctx, sb.ID)
	if err != nil {
		c.Log.Error("failed to read existing entity, trying with new definition", "id", sb.ID, "err", err)
		oldMeta = meta
	}

	var oldSb v1alpha.Sandbox
	oldSb.Decode(oldMeta)

	// TODO: handle adding a new container without destroying the sandbox first.
	if len(sb.Container) != len(oldSb.Container) {
		return false, nil, nil
	}

	for i, container := range sb.Container {
		if container.Name != oldSb.Container[i].Name {
			return false, nil, nil
		}

		if container.Image != oldSb.Container[i].Image {
			return false, nil, nil
		}

		if container.Command != oldSb.Container[i].Command {
			return false, nil, nil
		}

		if !slices.Equal(container.Env, oldSb.Container[i].Env) {
			return false, nil, nil
		}

		if !slices.Equal(container.Mount, oldSb.Container[i].Mount) {
			return false, nil, nil
		}

		if container.Privileged != oldSb.Container[i].Privileged {
			return false, nil, nil
		}

		if container.OomScore != oldSb.Container[i].OomScore {
			return false, nil, nil
		}
	}

	if !slices.Equal(sb.Port, oldSb.Port) {
		return false, nil, nil
	}

	return true, &oldSb, nil
}

func (c *SandboxController) containerId(id entity.Id) string {
	cid := id.String()
	cid = strings.TrimPrefix(cid, "sandbox/")
	return "sandbox." + cid
}

func (c *SandboxController) checkSandbox(ctx context.Context, co *v1alpha.Sandbox, meta *entity.Meta) (int, error) {
	c.Log.Debug("checking for existing sandbox", "id", co.ID)

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	cont, err := c.CC.LoadContainer(ctx, c.containerId(co.ID))
	if err != nil {
		return notFound, nil
	}

	labels, err := cont.Labels(ctx)
	if err != nil {
		return notFound, err
	}

	if _, ok := labels[sandboxVersionLabel]; !ok {
		c.Log.Debug("sandbox version label not found, assuming new sandbox")
		return differentVersion, nil
	}

	if labels[sandboxVersionLabel] != fmt.Sprint(meta.Revision) {
		c.Log.Debug("sandbox version mismatch", "expected", meta.Revision, "found", labels[sandboxVersionLabel])
		return differentVersion, nil
	}

	return same, nil
}

func (c *SandboxController) saveEntity(ctx context.Context, sb *v1alpha.Sandbox, meta *entity.Meta) error {
	path := c.sandboxPath(sb, "entity.cbor")

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create entity file: %w", err)
	}

	defer f.Close()

	data, err := entity.Encode(meta)
	if err != nil {
		return fmt.Errorf("failed to encode entity: %w", err)
	}

	_, err = f.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write entity file: %w", err)
	}

	return nil
}

func (c *SandboxController) readEntity(ctx context.Context, id entity.Id) (*entity.Meta, error) {
	path := filepath.Join(c.Tempdir, "containerd", id.PathSafe(), "entity.cbor")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("entity file not found: %w", err)
		}

		return nil, fmt.Errorf("failed to open entity file: %w", err)
	}

	var meta entity.Meta

	err = entity.Decode(data, &meta)
	if err != nil {
		return nil, fmt.Errorf("failed to decode entity file: %w", err)
	}

	return &meta, nil
}

func (c *SandboxController) updateSandbox(ctx context.Context, sb *v1alpha.Sandbox, meta *entity.Meta) error {
	// We support the ability to update a subnet of elements of the sandbox while running.
	// For everything else, we destroy it and rebuild it fully with Create.

	canUpdate, oldSb, err := c.canUpdateInPlace(ctx, sb, meta)
	if err != nil {
		c.Log.Error("failed to check if sandbox can be updated in place", "err", err)
	} else if canUpdate {

		cont, err := c.CC.LoadContainer(ctx, c.containerId(sb.ID))
		if err != nil {
			return fmt.Errorf("failed to load existing sandbox: %w", err)
		}

		if !slices.Equal(oldSb.Labels, sb.Labels) {
			labels, err := cont.Labels(ctx)
			if err != nil {
				return fmt.Errorf("failed to get container labels: %w", err)
			}

			for _, lbl := range oldSb.Labels {
				k, _, ok := strings.Cut(lbl, "=")
				if ok {
					delete(labels, strings.TrimSpace(k))
				}
			}

			for _, lbl := range sb.Labels {
				k, v, ok := strings.Cut(lbl, "=")
				if ok {
					labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}

			_, err = cont.SetLabels(ctx, labels)
			if err != nil {
				return err
			}
		}

		return c.saveEntity(ctx, sb, meta)
	}

	c.Log.Debug("destroying existing sandbox to recreate it")

	err = c.Delete(ctx, meta.ID)
	if err != nil {
		return fmt.Errorf("failed to delete existing sandbox: %w", err)
	}

	return c.createSandbox(ctx, sb, meta)
}

func (c *SandboxController) Create(ctx context.Context, co *v1alpha.Sandbox, meta *entity.Meta) error {
	searchRes, err := c.checkSandbox(ctx, co, meta)
	if err != nil {
		c.Log.Error("error checking sandbox, proceeding with create", "err", err)
	} else {
		switch searchRes {
		case same:
			c.Log.Debug("sandbox already exists, skipping create")
			return nil
		case differentVersion:
			return c.updateSandbox(ctx, co, meta)
		}
	}

	return c.createSandbox(ctx, co, meta)
}

func (c *SandboxController) createSandbox(ctx context.Context, co *v1alpha.Sandbox, meta *entity.Meta) error {
	c.Log.Debug("creating sandbox", "id", co.ID)

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	ep, err := c.allocateNetwork(ctx, co)
	if err != nil {
		return fmt.Errorf("failed to allocate network: %w", err)
	}

	opts, err := c.buildSpec(ctx, co, ep, meta)
	if err != nil {
		return fmt.Errorf("failed to build container spec: %w", err)
	}

	err = c.configureVolumes(ctx, co)
	if err != nil {
		return fmt.Errorf("failed to configure volumes: %w", err)
	}

	cid := c.containerId(co.ID)

	container, err := c.CC.NewContainer(ctx, cid, opts...)
	if err != nil {
		return errors.Wrapf(err, "failed to create container %s", co.ID)
	}

	defer func() {
		if err != nil {
			c.Log.Error("failed to create sandbox, cleaning up", "id", co.ID, "err", err)

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

	err = c.bootContainers(ctx, co, ep, int(task.Pid()))
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
			err := c.ResourcesMonitor.Monitor(c.topCtx, cid, cgroupPath)
			if err != nil {
				c.Log.Error("failed to monitor container resources", "id", co.ID, "err", err)
			}
		}()
	}

	c.Log.Info("sanbox started", "id", co.ID, "namespace", c.Namespace)

	return c.saveEntity(ctx, co, meta)
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
	meta *entity.Meta,
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

	for _, lbl := range co.Labels {
		if key, val, ok := strings.Cut(lbl, "="); ok {
			lbls[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}

	lbls[sandboxVersionLabel] = strconv.FormatInt(meta.Revision, 10)
	lbls[sandboxEntityLabel] = co.ID.String()

	//if config.StaticDir != "" {
	//lbls["runtime.computer/static_dir"] = config.StaticDir
	//}

	tmpDir := filepath.Join(c.Tempdir, "containerd", co.ID.PathSafe())
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
		containerdx.WithOOMScoreAdj(defaultSandboxOOMAdj, false),
	}

	cfg := map[string]string{}

	if co.HostNetwork {
		cfg["network"] = "host"
		specOpts = append(specOpts, oci.WithHostNamespace(specs.NetworkNamespace))
	}

	cfgPath := c.sandboxPath(co, "runsc.toml")

	err = c.setupNewRunscConfig(cfgPath, cfg)
	if err != nil {
		return nil, err
	}

	id := co.ID.String()

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runsc.v1", &runtimeoptions.Options{
			TypeUrl:    "io.containerd.runsc.v1.options",
			ConfigPath: cfgPath,
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

	err = c.configureFirewall(co, ep)
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
	c.Log.Info("booting containers", "count", len(sb.Container))

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	for _, container := range sb.Container {
		opts, err := c.buildSubContainerSpec(ctx, sb, &container, ep, sbPid)
		if err != nil {
			return fmt.Errorf("failed to build container spec: %w", err)
		}

		id := fmt.Sprintf("%s-%s", c.containerId(sb.ID), container.Name)

		c.Log.Info("creating container", "id", id)

		container, err := c.CC.NewContainer(ctx, id, opts...)
		if err != nil {
			return errors.Wrapf(err, "failed to create container %s", sb.ID)
		}

		task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
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

func (c *SandboxController) sandboxPath(sb *v1alpha.Sandbox, sub ...string) string {
	parts := append(
		[]string{c.Tempdir, "containerd", sb.ID.PathSafe()},
		sub...,
	)

	return filepath.Join(parts...)
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
	img, err := c.CC.GetImage(ctx, co.Image)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, co.Image, containerd.WithPullUnpack)
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", co.Image, err)
		}

		img, err = c.CC.GetImage(ctx, co.Image)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", co.Image, err)
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

	for _, m := range co.Mount {
		rawPath := c.sandboxPath(sb, "volumes", m.Source)
		st, err := os.Lstat(rawPath)
		if err != nil {
			return nil, fmt.Errorf("volume %s does not exist", rawPath)
		}

		for st.Mode().Type() == os.ModeSymlink {
			tgt, err := os.Readlink(rawPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read symlink %s: %w", rawPath, err)
			}

			rawPath = tgt
			st, err = os.Stat(rawPath)
			if err != nil {
				return nil, fmt.Errorf("volume %s does not exist", rawPath)
			}
		}

		mounts = append(mounts, specs.Mount{
			Destination: m.Destination,
			Type:        "bind",
			Source:      rawPath,
			Options:     []string{"rbind", "rw"},
		})
	}

	dir := co.Directory
	if dir == "" {
		dir = "/"
	}

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithProcessCwd(dir),
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
			"io.kubernetes.cri.sandbox-id":     c.containerId(sb.ID),
		}),
	}

	if co.OomScore != 0 {
		specOpts = append(specOpts, containerdx.WithOOMScoreAdj(int(co.OomScore), false))
	}

	if co.Privileged {
		specOpts = append(specOpts,
			oci.WithPrivileged,
			oci.WithAllDevicesAllowed,
			oci.WithWriteableCgroupfs,
			oci.WithAddedCapabilities([]string{"CAP_SYS_ADMIN"}),
		)
	}

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runsc.v1", &runtimeoptions.Options{
			TypeUrl:    "io.containerd.runsc.v1.options",
			ConfigPath: c.runscConfigPath,
		}),
	)

	return opts, nil
}

func (c *SandboxController) destroySubContainers(ctx context.Context, sb *v1alpha.Sandbox) error {
	// First, signal all the subcontainers with SIGTERM
	esCh := make(chan containerd.ExitStatus, len(sb.Container))

	var waiting int

	for _, container := range sb.Container {
		id := fmt.Sprintf("%s-%s", c.containerId(sb.ID), container.Name)

		c.Log.Debug("sending SIGTERM to subcontainer", "id", id)

		ctx = namespaces.WithNamespace(ctx, c.Namespace)

		cont, err := c.CC.LoadContainer(ctx, id)
		if err != nil {
			c.Log.Error("failed to load container", "id", id, "err", err)
			continue
		}

		task, err := cont.Task(ctx, nil)
		if err != nil {
			c.Log.Error("failed to load task", "id", id, "err", err)
		} else {
			ch, err := task.Wait(ctx)
			if err != nil {
				c.Log.Error("failed to get wait chan for task", "id", id, "err", err)
			} else {
				err = task.Kill(ctx, unix.SIGTERM)
				if err != nil {
					c.Log.Warn("failed to kill task", "id", id, "err", err)
				} else {
					waiting++

					go func() {
						select {
						case es := <-ch:
							esCh <- es
						case <-ctx.Done():
							esCh <- containerd.ExitStatus{}
						}
					}()
				}
			}
		}
	}

	ticker := time.NewTimer(5 * time.Second)
	defer ticker.Stop()

loop:
	for waiting > 0 {
		select {
		case <-ticker.C:
			c.Log.Debug("gave up waiting for containers to exit")
			break loop
		case <-ctx.Done():
			c.Log.Debug("context cancelled, giving up waiting for containers to exit")
			break loop
		case es := <-esCh:
			waiting--
			c.Log.Info("container exited", "exit_code", es.ExitCode())
		}
	}

	c.Log.Info("deleting subcontainers", "id", sb.ID, "containers", len(sb.Container))

	// Now, we can delete the subcontainers.
	for _, container := range sb.Container {
		id := fmt.Sprintf("%s-%s", c.containerId(sb.ID), container.Name)

		c.Log.Debug("destroying subcontainer", "id", id)

		ctx = namespaces.WithNamespace(ctx, c.Namespace)

		cont, err := c.CC.LoadContainer(ctx, id)
		if err != nil {
			c.Log.Error("failed to load container", "id", id, "err", err)
			continue
		}

		task, err := cont.Task(ctx, nil)
		if err != nil {
			c.Log.Error("failed to load task", "id", id, "err", err)
		} else {
			_, err = task.Delete(ctx, containerd.WithProcessKill)
			if err != nil {
				c.Log.Error("failed to delete task", "id", id, "err", err)
			}
		}

		err = cont.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			c.Log.Error("failed to delete container", "id", id, "err", err)
		}
	}

	return nil
}

func (c *SandboxController) Delete(ctx context.Context, id entity.Id) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	oldMeta, err := c.readEntity(ctx, id)
	if err != nil {
		c.Log.Error("failed to read existing entity", "id", id, "err", err)
		return fmt.Errorf("failed to read existing entity: %w", err)
	}

	var oldSb v1alpha.Sandbox
	oldSb.Decode(oldMeta)

	err = c.destroySubContainers(ctx, &oldSb)
	if err != nil {
		return fmt.Errorf("failed to destroy subcontainers: %w", err)
	}

	container, err := c.CC.LoadContainer(ctx, c.containerId(id))
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
	tmpDir := filepath.Join(c.Tempdir, "containerd", id.PathSafe())
	_ = os.RemoveAll(tmpDir)

	c.Log.Info("container stopped", "id", id)

	return nil
}
