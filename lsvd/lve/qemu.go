package lve

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"

	"log/slog"
)

type QemuInstance struct {
	options []string

	filesToOpen []string
}

func (q *QemuInstance) openFile(path string) int {
	idx := len(q.filesToOpen) + 3

	q.filesToOpen = append(q.filesToOpen, path)

	return idx
}

func (q *QemuInstance) setupOptions(log *slog.Logger, co *CommonOptions) error {
	s := strconv.Itoa
	sf := fmt.Sprintf

	q.options = append(q.options,
		"-smp", s(co.CPUs),
		"-m", s(co.MemoryMB),
		"-object", sf("memory-backend-file,id=ram,size=%dM,mem-path=%s,prealloc=on,share=on", co.MemoryMB, co.MemoryFile()),
		"-numa", "node,memdev=ram",
		"-device", sf("virtio-pstore,directory=%s/logs", co.WorkDir),
	)

	if co.UserNat {
		q.options = append(q.options,
			"-nic", "user",
		)
	}

	if co.MacVtap != "" {
		iface, err := net.InterfaceByName(co.MacVtap)
		if err != nil {
			return err
		}

		mac := iface.HardwareAddr.String()
		idx := iface.Index

		log.Info("configuring macvtap", "mac", mac, "idx", idx)

		q.options = append(q.options,
			"-nic", sf("tap,id=tap0,mac=%s,fd=%d", mac, q.openFile(sf("/dev/tap%d", idx))),
		)
	}

	q.options = append(q.options,
		"--drive", sf("file=nbd+unix:///%s?socket=%s,if=none,id=drive0", co.LSVDVolume, co.LSVDSocket),
		"--device", "virtio-blk-pci,drive=drive0,id=virtblk0,num-queues=4",
	)

	q.options = append(q.options,
		"--chardev", sf("socket,id=chr1,path=%s", co.LNFSocket),
		"--netdev", "type=vhost-user,id=net1,chardev=chr1",
		"--device", "virtio-net-pci,netdev=net1",
	)

	if co.HostMount != "" {
		q.options = append(q.options,
			"-virtfs", sf("local,path=%s,mount_tag=data,security_model=none", co.HostMount),
		)
	}

	return nil
}

func (q *QemuInstance) Start(ctx context.Context, log *slog.Logger, co *CommonOptions) error {
	q.options = append(q.options,
		"-cpu", "host",
		"-global", "ide-hd.physical_block_size=4096",
	)

	err := q.setupOptions(log, co)
	if err != nil {
		return err
	}

	q.options = append(q.options, "-vnc", "0.0.0.0:11")

	cmd := exec.CommandContext(ctx, "kvm", q.options...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	for _, path := range q.filesToOpen {
		fd, err := os.Open(path)
		if err != nil {
			return err
		}

		cmd.ExtraFiles = append(cmd.ExtraFiles, fd)
	}

	return cmd.Run()
}
