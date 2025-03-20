package container

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
	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/davecgh/go-spew/spew"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/netdb"

	"miren.dev/runtime/pkg/controller/container/v1alpha"
)

type ContainerController struct {
	Log *slog.Logger
	CC  *containerd.Client

	Namespace   string `asm:"namespace"`
	RunscBinary string `asm:"runsc_binary,optional"`
	Clickhouse  string `asm:"clickhouse-address"`

	NetServ *network.ServiceManager

	Bridge string `asm:"bridge-iface"`
	Subnet *netdb.Subnet

	DataPath string `asm:"data-path"`
	Tempdir  string `asm:"tempdir"`
}

func (c *ContainerController) Create(ctx context.Context, co *v1alpha.Container) error {

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

	/*
		spec, err := container.Spec(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed to get container spec %s", entity.ID)
		}

			config.Spec = spec

			config.CGroupPath = spec.Linux.CgroupsPath
	*/

	err = c.bootInitialTask(ctx, co, ep, container)
	if err != nil {
		task, _ := container.Task(ctx, nil)
		if task != nil {
			task.Delete(ctx, containerd.WithProcessKill)
		}

		derr := container.Delete(ctx, containerd.WithSnapshotCleanup)
		if derr != nil {
			c.Log.Error("failed to cleanup container", "id", co.ID, "err", derr)
		}
		return err
	}

	c.Log.Info("container started", "id", co.ID, "namespace", c.Namespace)

	return nil
}

func (c *ContainerController) Delete(ctx context.Context, entity *entity.Entity) error {
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

func (c *ContainerController) allocateNetwork(
	ctx context.Context,
	co *v1alpha.Container,
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

func (c *ContainerController) buildSpec(
	ctx context.Context,
	co *v1alpha.Container,
	ep *network.EndpointConfig,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, co.Image)
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

	lbls := map[string]string{}

	for _, lbl := range co.Label {
		if key, val, ok := strings.Cut(lbl, "="); ok {
			lbls[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}

	for _, p := range co.Port {
		proto := p.Protocol

		if proto == "" {
			proto = v1alpha.TCP
		}

		typ := p.Type
		if typ == "" {
			typ = "raw"
		}

		lbls[fmt.Sprintf("runtime.computer/port:%s", p.Name)] =
			fmt.Sprintf("port=%d,type=%s", p.Port, p.Type)

		/*
			if typ.Value.String() == "http" {
				lbls["runtime.computer/http_host"] = fmt.Sprintf("%s:%d", entity.Get(s.Id("address")).Value.String(), port.Value.Int64())
			}
		*/
	}

	//if config.StaticDir != "" {
	//lbls["runtime.computer/static_dir"] = config.StaticDir
	//}

	id := co.ID

	tmpDir := filepath.Join(c.Tempdir, "containerd", id)
	os.MkdirAll(tmpDir, 0755)

	resolvePath := filepath.Join(tmpDir, "resolv.conf")
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

	for _, m := range co.Mount {
		mounts = append(mounts, specs.Mount{
			Destination: m.Destination,
			Type:        "bind",
			Source:      m.Source,
			Options:     []string{"rbind", "rw"},
		})
	}

	var envs []string

	for _, v := range co.Env {
		envs = append(envs, v)
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

	if cmd := co.Command; cmd != "" {
		c.Log.Debug("overriding command", "command", cmd)
		specOpts = append(specOpts,
			oci.WithProcessArgs("/bin/sh", "-c", "exec "+cmd))
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
		containerd.WithRuntime("io.containerd.runc.v2", &options.Options{
			BinaryName: c.RunscBinary,
		}),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

func (c *ContainerController) writeResolve(path string, ep *network.EndpointConfig) error {
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

func (c *ContainerController) bootInitialTask(
	ctx context.Context,
	co *v1alpha.Container,
	ep *network.EndpointConfig,
	container containerd.Container,
) error {
	c.Log.Info("booting task")

	exe, err := exec.LookPath("containerd-log-ingress")
	if err != nil {
		return err
	}

	id := co.ID

	task, err := container.NewTask(ctx,
		cio.BinaryIO(exe, map[string]string{
			"-d": c.Clickhouse,
			"-e": id,
			"-l": "/tmp",
		}))
	if err != nil {
		return err
	}

	err = network.ConfigureNetNS(c.Log, int(task.Pid()), ep)
	if err != nil {
		return err
	}

	err = c.NetServ.SetupDNS(ep.Bridge)
	if err != nil {
		return err
	}

	c.Log.Warn("RIGHT BEFORE")
	spew.Dump(network.CGroupAddress(c.Log, int(task.Pid())))

	err = task.Start(ctx)
	if err != nil {
		return err
	}

	c.Log.Warn("RIGHT AFTER")
	spew.Dump(network.CGroupAddress(c.Log, int(task.Pid())))

	return nil
}
