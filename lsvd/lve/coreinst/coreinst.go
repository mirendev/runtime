package coreinst

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"miren.dev/runtime/lsvd/lve/gatewaymgr"
	"miren.dev/runtime/lsvd/lve/paths"
	"miren.dev/runtime/lsvd/lve/pkg/id"
)

type CoreConfig struct {
	Id id.Id `json:"id"`

	CPUs   int `json:"cpus"`
	Memory int `json:"memory"`

	Interfaces []*gatewaymgr.InterfaceConfig `json:"interfaces"`
}

/*

port=8989
# ssh_key=url
kvm -cpu host -M q35,acpi=off \
        -global ide-hd.physical_block_size=4096 \
        -smp 2 -m 256 \
        -object memory-backend-file,id=ram,size=256M,mem-path=./gw-mem,prealloc=on,share=on \
        -numa node,memdev=ram \
        -nodefaults -no-user-config \
        -nographic \
        -chardev stdio,id=vc0 \
        -device virtio-serial-pci \
        -device virtconsole,chardev=vc0 \
        -kernel vmlinuz -initrd initrd \
        -append "console=hvc0 root=/dev/vda overlaytmpfs config_vol=config" \
        -virtfs local,path="$(pwd)/mktmp/config",mount_tag=config,security_model=mapped-xattr \
        -drive file=os.fs,format=raw,if=none,id=boot \
        -device virtio-blk-pci,drive=boot \
        -netdev tap,id=t0,fd=3 \
        -device virtio-net-pci,netdev=t0,mac="$(cat /sys/class/net/macvtap1/address)" \
        -chardev socket,id=chr1,path=./sockets/gw.sock,server=on,wait=on \
        -netdev vhost-user,id=net1,chardev=chr1 \
        -device virtio-net-pci,netdev=net1,mac=ba:cd:b3:d6:da:ea 3<>/dev/tap"$(cat /sys/class/net/macvtap1/ifindex)"

*/

func BootCoreInst(ctx context.Context, log *slog.Logger, cfg *CoreConfig, rootDir string) error {
	root := paths.Root(rootDir)

	dir := root.Instance(cfg.Id)

	s := strconv.Itoa
	sf := fmt.Sprintf

	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	/*


		port=8989
		# ssh_key=url
		kvm -cpu host -M q35 \
		        -global ide-hd.physical_block_size=4096 \
		        -smp 2 -m 256 \
		        -object memory-backend-file,id=ram,size=256M,mem-path=./mem-$$,prealloc=on,share=on \
		        -numa node,memdev=ram \
		        -nic user \
		        -nographic \
		        -drive file=os.fs,format=raw,if=none,id=boot,read-only \
		        -device virtio-blk-pci,drive=boot \
		        -kernel vmlinuz -initrd initrd \
		        -append "console=ttyS0 root=/dev/vda overlaytmpfs" \
		        -chardev socket,id=chr1,path=./sockets/gw-$$.sock,server=on,wait=on \
		        -netdev vhost-user,id=net1,chardev=chr1 \
		        -device virtio-net-pci,netdev=net1

	*/

	pidPath := root.InstancePid(cfg.Id)
	pidFile, err := os.ReadFile(pidPath)
	if err == nil {
		return fmt.Errorf("instance managed by pid %s", strings.TrimSpace(string(pidFile)))
	}

	defer os.Remove(pidPath)
	f, err := os.OpenFile(pidPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0644)
	if err != nil {
		return errors.Wrapf(err, "attempting to create pid path")
	}

	fmt.Fprintf(f, "%d\n", os.Getpid())

	f.Close()

	args := []string{
		"-cpu", "host",
		"-M", "q35",
		"-global", "ide-hd.physical_block_size=4096",
		"-nographic",

		"-smp", s(cfg.CPUs),
		"-m", s(cfg.Memory),

		"-object", sf("memory-backend-file,id=ram,size=%dM,mem-path=%s/memory,prealloc=on,share=on", cfg.Memory, dir),
		"-numa", "node,memdev=ram",

		"-kernel", root.CoreData("vmlinuz"),
		"-initrd", root.CoreData("initrd"),

		"-append", "console=ttyS0 root=/dev/vda overlaytmpfs config_vol=config",

		/*
			"-chardev", sf("socket,id=vc0,path=%s/console.sock,server=on,wait=off", dir),
			"-device", "virtio-serial-pci",
			"-device", "virtconsole,chardev=vc0",
		*/

		"-virtfs", sf("local,path=%s/config,mount_tag=config,security_model=mapped-xattr", dir),
		"-drive", sf("file=%s,format=raw,if=none,id=boot,readonly", root.CoreData("os.fs")),
		"-device", "virtio-blk-pci,drive=boot",

		/*
			"-chardev", sf("socket,id=mon0,path=%s/qmp.sock,server=on,wait=off", dir),
			"-mon", "mon0,mode=control,pretty=on",
		*/
	}

	var toOpen []string

	for i, inf := range cfg.Interfaces {
		if inf.HostTap != "" {
			args = append(args,
				"-netdev", sf("tap,id=inf%d,fd=3", i),
				"-device", sf("virtio-net-pci,netdev=inf%d,mac=%s", i, inf.Id.MacString()),
			)

			hostif, err := net.InterfaceByName(inf.HostTap)
			if err != nil {
				return err
			}

			toOpen = append(toOpen, sf("/dev/tap%d", hostif.Index))

			log.Info("configured interface for mactap", "host", inf.HostTap, "index", hostif.Index)
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

	cmd := exec.CommandContext(ctx, "kvm", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	for _, path := range toOpen {
		fd, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}

		cmd.ExtraFiles = append(cmd.ExtraFiles, fd)
	}

	return cmd.Run()
}
