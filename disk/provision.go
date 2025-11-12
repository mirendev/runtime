package disk

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
)

type Provisioner struct {
	Log     *slog.Logger
	CC      *containerd.Client
	NetServ *network.ServiceManager

	//Image   string `asm:"lsvd-image"`
	Tempdir string `asm:"tempdir"`

	Clickhouse string `asm:"clickhouse-address,optional"`
	Namespace  string `asm:"namespace"`
	Bridge     string `asm:"bridge-iface"`
	Subnet     *netdb.Subnet
	Port       int `asm:"server_port"`
}

var _ = autoreg.Register[Provisioner]()

func (p *Provisioner) Init(ctx context.Context) error {
	bc := &network.BridgeConfig{
		Name:      p.Bridge,
		Addresses: []netip.Prefix{p.Subnet.Router()},
	}

	err := p.NetServ.SetupDNS(ctx, bc)
	if err != nil {
		return err
	}

	return nil
}

type ProvisionConfig struct {
	Name      string
	DataDir   string
	AccessDir string
	LogFile   string
}

func (p *Provisioner) Provision(ctx context.Context, pc ProvisionConfig) error {
	ctx = namespaces.WithNamespace(ctx, p.Namespace)
	ref := "docker.io/library/ubuntu:latest"
	img, err := p.CC.GetImage(ctx, ref)
	if err != nil {
		img, err = p.CC.Pull(ctx, ref, containerd.WithPullUnpack)
		if err != nil {
			return err
		}
	}

	id := idgen.Gen("s")

	p.Log.Info("provisioning container", "id", id, "name", pc.Name)

	ec, err := network.AllocateOnBridge(p.Bridge, p.Subnet)
	if err != nil {
		return err
	}

	p.Log.Info("allocated network", "id", id, "endpoint", ec.Addresses[0].String())

	tmpDir := filepath.Join(p.Tempdir, "containerd", id)
	os.MkdirAll(tmpDir, 0755)

	resolvePath := filepath.Join(tmpDir, "resolv.conf")
	err = p.writeResolve(resolvePath, ec)
	if err != nil {
		return err
	}

	lbls := map[string]string{
		"runtime.computer/storage":    "disk",
		"runtime.computer/name":       pc.Name,
		"rpc.runtime.computer/access": "stdio,udp",
	}

	src, err := os.Executable()
	if err != nil {
		return err
	}

	mounts := []specs.Mount{
		/*
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
		*/
		{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      resolvePath,
			Options:     []string{"rbind", "rw"},
		},
		{
			Destination: "/bin/miren",
			Type:        "bind",
			Source:      src,
			Options:     []string{"rbind", "ro"},
		},
		{
			Destination: "/data",
			Type:        "bind",
			Source:      pc.DataDir,
			Options:     []string{"rbind", "rw"},
		},
		{
			Destination: "/access",
			Type:        "bind",
			Source:      pc.AccessDir,
			Options:     []string{"rbind", "rshared"},
		},
	}

	_ = mounts

	dsAddr := fmt.Sprintf("https://%s:%d/dataset", p.Subnet.Router().Addr(), p.Port)

	p.Log.Info("using dataset uri", "uri", dsAddr)

	specOpts := []oci.SpecOpts{
		oci.WithDefaultSpec(),
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithHostHostsFile,
		//oci.WithEnv(envs),
		//oci.WithHostResolvconf,
		//oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		//oci.WithProcessCwd("/app"),
		oci.WithProcessArgs("/bin/miren", "disk", "run", "-v",
			"--data", "/data",
			"-d", "/access",
			"--mount", "/access/fs",
			"--dataset", dsAddr,
			"-n", pc.Name),
		oci.WithPrivileged,
		oci.WithAllDevicesAllowed,

		func(ctx context.Context, c1 oci.Client, c2 *containers.Container, s *oci.Spec) error {
			// TODO get a proper way to set this flag upstream in containerd
			s.Linux.RootfsPropagation = "shared"
			return nil
		},
		//oci.WithWriteableCgroupfs,
		//oci.WithAddedCapabilities([]string{"CAP_SYS_ADMIN"}),
	}

	var (
		opts []containerd.NewContainerOpts
	)

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runc.v2", nil),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	container, err := p.CC.NewContainer(ctx, id, opts...)
	if err != nil {
		return errors.Wrapf(err, "failed to create container %s", id)
	}

	err = p.bootInitialTask(ctx, id, ec, container, pc)
	if err != nil {
		task, _ := container.Task(ctx, nil)
		if task != nil {
			task.Delete(ctx, containerd.WithProcessKill)
		}

		derr := container.Delete(ctx, containerd.WithSnapshotCleanup)
		if derr != nil {
			p.Log.Error("failed to cleanup container", "id", id, "err", derr)
		}
		return err
	}

	p.Log.Info("container started", "id", id, "namespace", p.Namespace)

	return nil
}

func (c *Provisioner) writeResolve(path string, ec *network.EndpointConfig) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(ec.Bridge.Addresses) == 0 {
		return fmt.Errorf("no nameservers available in bridge config")
	}

	for _, addr := range ec.Bridge.Addresses {
		if !addr.Addr().IsValid() {
			return fmt.Errorf("invalid nameserver address: %v", addr)
		}
		fmt.Fprintf(f, "nameserver %s\n", addr.Addr().String())
	}

	return nil
}

func (c *Provisioner) bootInitialTask(
	ctx context.Context,
	id string,
	ep *network.EndpointConfig,
	container containerd.Container,
	pc ProvisionConfig,
) error {
	c.Log.Info("booting task")

	var io cio.Creator

	if pc.LogFile != "" {
		io = cio.LogFile(pc.LogFile)
	} else {
		exe, err := exec.LookPath("containerd-log-ingress")
		if err != nil {
			return err
		}

		io = cio.BinaryIO(exe, map[string]string{
			"-d": c.Clickhouse,
			"-e": id,
			"-l": pc.AccessDir,
		})
	}

	task, err := container.NewTask(ctx, io)
	if err != nil {
		return err
	}

	c.Log.Info("task created", "pid", task.Pid())

	err = network.ConfigureNetNS(c.Log, int(task.Pid()), ep)
	if err != nil {
		return err
	}

	return task.Start(ctx)
}
