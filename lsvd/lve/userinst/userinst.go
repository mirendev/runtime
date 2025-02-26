package userinst

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"miren.dev/runtime/lsvd/lve/gatewaymgr"
	"miren.dev/runtime/lsvd/lve/paths"
	"miren.dev/runtime/lsvd/lve/pkg/id"
	"miren.dev/runtime/lsvd/lve/pkg/qmp"
)

type UserConfig struct {
	Id id.Id `json:"id"`

	CPUs   int `json:"cpus"`
	Memory int `json:"memory"`

	Volume string `json:"volume"`
	DiskId id.Id  `json:"disk"`

	Interfaces []*gatewaymgr.InterfaceConfig `json:"interfaces"`

	UserData string `json:"userdata"`
}

func BootInst(ctx context.Context, log *slog.Logger, cfg *UserConfig, rootDir string) error {

	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}

	root := paths.Root(rootDir)

	err = root.SetupInstance(cfg.Id)
	if err != nil {
		return err
	}

	cidataPath := root.InstanceCidata(cfg.Id)

	err = WriteCloudInit(ctx, log, cfg, cidataPath)
	if err != nil {
		return err
	}

	dir := root.Instance(cfg.Id)

	s := strconv.Itoa
	sf := fmt.Sprintf

	args := []string{
		"-cpu", "host",
		"-M", "q35",
		"-global", "ide-hd.physical_block_size=4096",
		"-nographic",

		"-smp", s(cfg.CPUs),
		"-m", s(cfg.Memory),

		"-object", sf("memory-backend-file,id=ram,size=%dM,mem-path=%s/memory,prealloc=on,share=on", cfg.Memory, dir),
		"-numa", "node,memdev=ram",

		"-drive", sf("file=%s,media=cdrom", cidataPath),

		"-drive", sf("file=nbd+unix:///%s?socket=%s,if=none,id=drive0", cfg.DiskId.String(), root.DiskSocket(cfg.DiskId)),
		"-device", "virtio-blk-pci,drive=drive0,id=virtblk0,num-queues=4",

		/*
			"-chardev", sf("socket,id=vc0,path=%s/console.sock,server=on,wait=off", dir),
			"-device", "virtio-serial-pci",
			"-device", "virtconsole,chardev=vc0",
		*/

		"-chardev", sf("socket,id=mon0,path=%s/qmp.sock,server=on,wait=off", dir),
		"-mon", "mon0,mode=control,pretty=on",
	}

	for i, inf := range cfg.Interfaces {
		if inf.HostTap != "" {
			return fmt.Errorf("user instances are not allowed to use host-tap")
		} else {
			path := root.VPCInterface(inf.VPC, inf.Id)
			defer os.Remove(path)

			args = append(args,
				"-chardev", sf("socket,id=infchr%d,path=%s,server=on,wait=on", i, path),
				"-netdev", sf("vhost-user,id=inf%d,chardev=infchr%d", i, i),
				"-device", sf("virtio-net-pci,netdev=inf%d,mac=%s", i, inf.Id.MacString()),
			)

			log.Info("configured interface for lnf", "path", path)
		}
	}

	log.Info("booting instance", "id", cfg.Id)

	sub, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(sub, "kvm", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err = cmd.Start()
	if err != nil {
		return err
	}

	events := make(chan *qmp.Packet)

	var d net.Dialer

	for {
		c, err := d.DialContext(ctx, "unix", filepath.Join(dir, "qmp.sock"))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		qconn, err := qmp.Open(log, c, events)
		if err != nil {
			log.Warn("unable to connect to qmp", "error", err)
			c.Close()
			time.Sleep(time.Second)
			continue
		}

		err = qconn.Watch(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}

			log.Warn("error negotiating with qemu via qmp", "error", err)
			c.Close()
			time.Sleep(time.Second)
			continue
		}

		val, err := qconn.Execute(ctx, "query-version", nil)
		if err != nil {
			log.Warn("error querying qmp version", "error", err)
			c.Close()
			time.Sleep(time.Second)
			continue
		}

		spew.Dump(val)

		for {
			select {
			case <-ctx.Done():
				log.Warn("context closed, issuing powerdown")
				qconn.ExecuteNR(context.Background(), "system_powerdown", nil)
				return cmd.Wait()
			case ev := <-events:
				spew.Dump(ev)
			}
		}
	}
}
