package disk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/lsvd/pkg/ext4"
	"miren.dev/runtime/lsvd/pkg/nbd"
	"miren.dev/runtime/lsvd/pkg/nbdnl"
	"miren.dev/runtime/pkg/fsmon"
)

type Runner struct {
	dir string
	log *slog.Logger
	sa  lsvd.SegmentAccess

	fsPath  string
	cleanup func() error
	d       *lsvd.Disk
}

func NewRunner(sa lsvd.SegmentAccess, dir string, log *slog.Logger) (*Runner, error) {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}

	return &Runner{
		dir: dir,
		log: log,
		sa:  sa,
	}, nil
}

func (r *Runner) cachePath() string {
	return filepath.Join(r.dir, "cache")
}

func (r *Runner) socketAddr() string {
	return filepath.Join(r.dir, "disk.sock")
}

func (r *Runner) devPath() string {
	return filepath.Join(r.dir, "dev")
}

func (r *Runner) idxPath() string {
	return filepath.Join(r.dir, "idx")
}

const UmountSignalTimeout = 5 * time.Minute

func (r *Runner) Start(ctx context.Context, name, fsPath, bindAddr string) error {
	r.fsPath = fsPath

	os.MkdirAll(r.cachePath(), 0755)

	diskOpts := []lsvd.Option{
		lsvd.WithSegmentAccess(r.sa),
		lsvd.WithVolumeName(name),
		lsvd.EnableAutoGC,
	}

	d, err := lsvd.NewDisk(ctx, r.log, r.cachePath(), diskOpts...)
	if err != nil {
		return err
	}

	r.d = d

	vol, err := r.sa.GetVolumeInfo(ctx, name)
	if err != nil {
		return fmt.Errorf("unable to locate volume: %s", err)
	}

	idx, conn, _, cleanup, err := nbdnl.Loopback(ctx, uint64(vol.Size))
	if err != nil {
		return errors.Wrapf(err, "setting up loopback")
	}

	r.cleanup = cleanup

	os.WriteFile(r.idxPath(), []byte(strconv.Itoa(int(idx))), 0644)

	devPath := r.devPath()

	os.Remove(devPath)

	nbdRng, err := nbdRange()
	if err != nil {
		return errors.Wrapf(err, "getting nbd range")
	}

	err = unix.Mknod(devPath, unix.S_IFBLK|0600, int(unix.Mkdev(43, idx*uint32(nbdRng))))
	if err != nil {
		return errors.Wrapf(err, "creating device node")
	}

	log := r.log

	log.Debug("loopback setup", "index", idx)

	nbdOpts := &nbd.Options{
		MinimumBlockSize:   4096,
		PreferredBlockSize: 4096,
		MaximumBlockSize:   4096,
	}

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		defer cancel()
		err := nbd.HandleTransport(log, conn, lsvd.NBDWrapper(ctx, log, d), nbdOpts)
		if err != nil {
			log.Error("error handling nbd", "error", err)
		}
	}()

	err = os.MkdirAll(fsPath, 0755)
	if err != nil {
		cleanup()
		return errors.Wrapf(err, "creating fs path")
	}

	sb, err := ext4.ReadExt4SuperBlock(devPath)
	if err != nil {
		log.Info("error reading superblock, formatting drive", "error", err)

		out, err := exec.Command("mkfs.ext4",
			"-b", "4096", "-m", "0",
			devPath).CombinedOutput()
		if err != nil {
			os.Stdout.Write(out)

			cleanup()
			return errors.Wrapf(err, "formatting ext4")
		}

		sb, err = ext4.ReadExt4SuperBlock(devPath)
		if err != nil {
			log.Info("error reading superblock, formatting drive", "error", err)
		}
	} else {
		cmd := exec.Command("e2fsck", "-f", "-y", devPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				if ee.ExitCode() > 2 {
					return errors.Wrapf(err, "fscking")
				}
			}
		}

		normSize := vol.Size - (vol.Size % 4096)
		fsSize, _ := ext4.Size(sb)

		diff := normSize - fsSize

		if diff > 0 && diff > (4096*4) {
			log.Info("resizing filesystem", "current", fsSize, "new", normSize)

			out, err := exec.Command("resize2fs", devPath).CombinedOutput()
			if err != nil {
				os.Stdout.Write(out)
				return errors.Wrapf(err, "resizing ext4")
			}

			os.Stdout.Write(out)

			sb, err = ext4.ReadExt4SuperBlock(devPath)
			if err != nil {
				log.Info("error reading superblock, formatting drive", "error", err)
			}
		}
	}

	fsSize, _ := ext4.Size(sb)

	log.Info("ext4 filesystem ready", "size", fsSize, "mount-point", fsPath)

	err = mountDisk(devPath, fsPath)
	if err != nil {
		spew.Dump(err)
		log.Info("error mounting", "error", err, "dev", devPath, "fs", fsPath)
	}

	go r.Serve(ctx, cancel, log, bindAddr)

	return nil
}

func (r *Runner) Cleanup() error {
	defer func() {
		r.log.Info("closing disk", "timeout", "5m")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		r.d.Close(ctx)
	}()

	defer unix.Unmount(r.fsPath, 0)

	log := r.log

	log.Info("performing shutdown operations")

	var lastWarning, lastFiles time.Time

	lastSignal := time.Now()

	for {
		err := unix.Unmount(r.fsPath, 0)
		if err == nil {
			break
		}

		if errors.Is(err, unix.EBUSY) {
			if time.Since(lastWarning) > 10*time.Second {
				lastWarning = time.Now()
				log.Warn("disk is busy, waiting before trying again")
			}

			if time.Since(lastFiles) > 30*time.Second {
				lastFiles = time.Now()
				fa, err := fsmon.AccessUnder(r.devPath())
				if err != nil {
					log.Warn("unable to enumerate open files", "error", err)
				} else {
					if len(fa) == 0 {
						log.Warn("no open files detected")
					} else {
						for _, a := range fa {
							log.Warn("file open preventing umount", "path", a.FilePath, "pid", a.PID, "process", a.ProcessName)
						}

						if time.Since(lastSignal) > UmountSignalTimeout {
							lastSignal = time.Now()

							log.Warn("sending SIGTERM to all accessing processes")
							for _, a := range fa {
								if pid, err := strconv.Atoi(a.PID); err == nil {
									unix.Kill(pid, unix.SIGTERM)
									log.Warn("sent SIGTERM", "pid", pid)
								}
							}
						}
					}
				}
			}

			time.Sleep(time.Second)
		} else {
			log.Error("error unmounting", "error", err)
			return err
		}
	}

	err := r.cleanup()
	if err != nil {
		log.Info("error cleaning up", "error", err)
	}

	return err
}

func (r *Runner) Run(ctx context.Context, name, fsPath, bindAddr string) error {
	os.MkdirAll(r.cachePath(), 0755)

	diskOpts := []lsvd.Option{
		lsvd.WithSegmentAccess(r.sa),
		lsvd.WithVolumeName(name),
		lsvd.EnableAutoGC,
	}

	d, err := lsvd.NewDisk(ctx, r.log, r.cachePath(), diskOpts...)
	if err != nil {
		return err
	}

	defer func() {
		r.log.Info("closing disk", "timeout", "5m")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		d.Close(ctx)
	}()

	vol, err := r.sa.GetVolumeInfo(ctx, name)
	if err != nil {
		return fmt.Errorf("unable to locate volume: %s", err)
	}

	idx, conn, _, cleanup, err := nbdnl.Loopback(ctx, uint64(vol.Size))
	if err != nil {
		return errors.Wrapf(err, "setting up loopback")
	}

	os.WriteFile(r.idxPath(), []byte(strconv.Itoa(int(idx))), 0644)

	devPath := r.devPath()

	os.Remove(devPath)

	nbdRng, err := nbdRange()
	if err != nil {
		return errors.Wrapf(err, "getting nbd range")
	}

	err = unix.Mknod(devPath, unix.S_IFBLK|0600, int(unix.Mkdev(43, idx*uint32(nbdRng))))
	if err != nil {
		return errors.Wrapf(err, "creating device node")
	}

	log := r.log

	log.Debug("loopback setup", "index", idx)

	nbdOpts := &nbd.Options{
		MinimumBlockSize:   4096,
		PreferredBlockSize: 4096,
		MaximumBlockSize:   4096,
	}

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		defer cancel()
		err := nbd.HandleTransport(log, conn, lsvd.NBDWrapper(ctx, log, d), nbdOpts)
		if err != nil {
			log.Error("error handling nbd", "error", err)
		}
	}()

	err = os.MkdirAll(fsPath, 0755)
	if err != nil {
		return errors.Wrapf(err, "creating fs path")
	}

	defer unix.Unmount(fsPath, 0)

	sb, err := ext4.ReadExt4SuperBlock(devPath)
	if err != nil {
		log.Info("error reading superblock, formatting drive", "error", err)

		out, err := exec.Command("mkfs.ext4",
			"-b", "4096", "-m", "0",
			devPath).CombinedOutput()
		if err != nil {
			return errors.Wrapf(err, "formatting ext4")
		}

		os.Stdout.Write(out)

		sb, err = ext4.ReadExt4SuperBlock(devPath)
		if err != nil {
			log.Info("error reading superblock, formatting drive", "error", err)
		}
	} else {
		cmd := exec.Command("e2fsck", "-f", "-y", devPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				if ee.ExitCode() > 2 {
					return errors.Wrapf(err, "fscking")
				}
			}
		}

		normSize := vol.Size - (vol.Size % 4096)
		fsSize, _ := ext4.Size(sb)

		diff := normSize - fsSize

		if diff > 0 && diff > (4096*4) {
			log.Info("resizing filesystem", "current", fsSize, "new", normSize)

			out, err := exec.Command("resize2fs", devPath).CombinedOutput()
			if err != nil {
				os.Stdout.Write(out)
				return errors.Wrapf(err, "resizing ext4")
			}

			os.Stdout.Write(out)

			sb, err = ext4.ReadExt4SuperBlock(devPath)
			if err != nil {
				log.Info("error reading superblock, formatting drive", "error", err)
			}
		}
	}

	fsSize, _ := ext4.Size(sb)

	log.Info("ext4 filesystem ready", "size", fsSize)

	err = mountDisk(devPath, fsPath)
	if err != nil {
		spew.Dump(err)
		log.Info("error mounting", "error", err, "dev", devPath, "fs", fsPath)
	}

	go r.Serve(ctx, cancel, log, bindAddr)

	<-ctx.Done()

	log.Info("performing shutdown operations")

	var lastWarning, lastFiles time.Time

	lastSignal := time.Now()

	for {
		err = unix.Unmount(fsPath, 0)
		if err == nil {
			break
		}

		if errors.Is(err, unix.EBUSY) {
			if time.Since(lastWarning) > 10*time.Second {
				lastWarning = time.Now()
				log.Warn("disk is busy, waiting before trying again")
			}

			if time.Since(lastFiles) > 30*time.Second {
				lastFiles = time.Now()
				fa, err := fsmon.AccessUnder(devPath)
				if err != nil {
					log.Warn("unable to enumerate open files", "error", err)
				} else {
					if len(fa) == 0 {
						log.Warn("no open files detected")
					} else {
						for _, a := range fa {
							log.Warn("file open preventing umount", "path", a.FilePath, "pid", a.PID, "process", a.ProcessName)
						}

						if time.Since(lastSignal) > UmountSignalTimeout {
							lastSignal = time.Now()

							log.Warn("sending SIGTERM to all accessing processes")
							for _, a := range fa {
								if pid, err := strconv.Atoi(a.PID); err == nil {
									unix.Kill(pid, unix.SIGTERM)
									log.Warn("sent SIGTERM", "pid", pid)
								}
							}
						}
					}
				}
			}

			time.Sleep(time.Second)
		} else {
			log.Error("error unmounting", "error", err)
			return err
		}
	}

	err = cleanup()
	if err != nil {
		log.Info("error cleaning up", "error", err)
	}

	err = ctx.Err()
	if err == context.Canceled {
		return nil
	}

	return err
}

func (r *Runner) prepDisk() error {
	return nil
}

func nbdRange() (int, error) {
	data, err := os.ReadFile("/sys/dev/block/43:0/range")
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(string(data)))
}
