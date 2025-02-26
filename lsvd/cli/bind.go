package cli

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/lsvd/paths"
	"miren.dev/runtime/lsvd/pkg/ext4"
	"miren.dev/runtime/lsvd/pkg/id"
	"miren.dev/runtime/lsvd/pkg/nbd"
	"miren.dev/runtime/lsvd/pkg/nbdnl"
	"miren.dev/runtime/pkg/units"
)

func nbdRange() (int, error) {
	data, err := os.ReadFile("/sys/dev/block/43:0/range")
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func (c *CLI) bind(ctx context.Context, opts struct {
	Global
	Path        string `short:"p" long:"path" description:"path for lve root" required:"true"`
	BindPath    string `short:"b" long:"bind" description:"path to bind to"`
	MetricsAddr string `long:"metrics" default:":2121" description:"address to expose metrics on"`
	Id          id.Id  `short:"i" long:"id" description:"identifier of disk"`
	Size        string `short:"s" long:"size" description:"size to advertise the volume as"`
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	log := c.log
	path := opts.Path

	var diskPath string

	name := opts.Id.String()

	root := paths.Root(path)

	err = root.SetupDisk(opts.Id)
	if err != nil {
		return err
	}

	diskPath = root.DiskData(opts.Id, "cache")

	size := units.MegaBytes(1024).Bytes()
	/*
		size, err := parseSize(opts.Size)
		if err != nil {
			return err
		}
	*/

	vol, err := sa.GetVolumeInfo(ctx, name)
	if err != nil {
		c.log.Info("unable to existing volume, creating one", "error", err)

		u, err := uuid.NewRandom()
		if err != nil {
			return err
		}

		err = sa.InitVolume(ctx, &lsvd.VolumeInfo{
			Name: name,
			Size: size,
			UUID: u.String(),
		})
		if err != nil {
			return errors.Wrapf(err, "creating new volume")
		}

		vol, err = sa.GetVolumeInfo(ctx, name)
		if err != nil {
			return errors.Wrapf(err, "loading new volume")
		}
	} else {
		if vol.Size < size {
			// TODO: Support resizing volumes down.
			c.log.Info("resizing volume metadata", "old", vol.Size, "new", size)
		}

		vol.Size = size

		err = sa.InitVolume(ctx, vol)
		if err != nil {
			return errors.Wrapf(err, "creating new volume")
		}

		vol, err = sa.GetVolumeInfo(ctx, name)
		if err != nil {
			return errors.Wrapf(err, "loading new volume")
		}
	}

	nbdRng, err := nbdRange()
	if err != nil {
		return errors.Wrapf(err, "getting nbd range")
	}

	c.log.Info("volume info", "size", vol.Size, "name", vol.Name, "uuid", vol.UUID)

	diskOpts := []lsvd.Option{
		lsvd.WithSegmentAccess(sa),
		lsvd.WithVolumeName(name),
		lsvd.EnableAutoGC,
	}

	d, err := lsvd.NewDisk(ctx, log, diskPath, diskOpts...)
	if err != nil {
		log.Error("error creating new disk", "error", err)
		os.Exit(1)
	}

	ch := make(chan os.Signal, 1)

	go func() {
		for range ch {
			log.Info("closing segment by signal request")
			d.CloseSegment(ctx)
		}
	}()

	signal.Notify(ch, unix.SIGHUP)

	defer func() {
		log.Info("closing disk", "timeout", "5m")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		d.Close(ctx)
	}()

	http.Handle("/metrics", promhttp.Handler())
	// Will also include pprof via the init() in net/http/pprof
	go http.ListenAndServe(opts.MetricsAddr, nil)

	idx, conn, cleanup, err := nbdnl.Loopback(ctx, uint64(vol.Size))
	if err != nil {
		log.Error("error setting up loopback", "error", err)
		os.Exit(1)
	}

	devPath := filepath.Join(path, "dev")

	os.Remove(devPath)

	err = unix.Mknod(devPath, unix.S_IFBLK|0600, int(unix.Mkdev(43, idx*uint32(nbdRng))))
	if err != nil {
		return errors.Wrapf(err, "creating device node")
	}

	log.Debug("loopback setup", "index", idx)

	nbdOpts := &nbd.Options{
		MinimumBlockSize:   4096,
		PreferredBlockSize: 4096,
		MaximumBlockSize:   4096,
	}

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		defer cancel()
		log.Info("handling nbd")
		err := nbd.HandleTransport(log, conn, lsvd.NBDWrapper(ctx, log, d), nbdOpts)
		if err != nil {
			log.Error("error handling nbd", "error", err)
		}
	}()

	c.log.Info("waiting on nbd", "path", opts.BindPath, "dev", devPath)

	defer unix.Unmount(opts.BindPath, 0)

	sb, err := ext4.ReadExt4SuperBlock(devPath)
	if err != nil {
		c.log.Info("error reading superblock, formatting drive", "error", err)

		out, err := exec.Command("mkfs.ext4", devPath).CombinedOutput()
		if err != nil {
			return errors.Wrapf(err, "formatting ext4")
		}

		os.Stdout.Write(out)

		sb, err = ext4.ReadExt4SuperBlock(devPath)
		if err != nil {
			c.log.Info("error reading superblock, formatting drive", "error", err)
		}
	} else {
		cmd := exec.Command("e2fsck", "-f", "-y", devPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return errors.Wrapf(err, "fscking")
		}

		normSize := size - (size % 4096)
		fsSize, _ := ext4.Size(sb)

		diff := normSize - fsSize

		if diff > 0 && diff > (4096*4) {
			c.log.Info("resizing filesystem", "current", fsSize, "new", normSize)

			out, err := exec.Command("resize2fs", devPath).CombinedOutput()
			if err != nil {
				os.Stdout.Write(out)
				return errors.Wrapf(err, "resizing ext4")
			}

			os.Stdout.Write(out)

			sb, err = ext4.ReadExt4SuperBlock(devPath)
			if err != nil {
				c.log.Info("error reading superblock, formatting drive", "error", err)
			}
		}
	}

	fsSize, _ := ext4.Size(sb)

	c.log.Info("ext4 filesystem ready", "size", fsSize)

	err = unix.Mount(devPath, opts.BindPath, "ext4", 0, "")
	if err != nil {
		c.log.Info("error mounting", "error", err)
	}

	<-ctx.Done()

	c.log.Info("performing shutdown operations")
	err = unix.Unmount(opts.BindPath, 0)
	if err != nil {
		c.log.Info("error unmounting", "error", err)
	}

	err = cleanup()
	if err != nil {
		c.log.Info("error cleaning up", "error", err)
	}

	err = ctx.Err()
	if err == context.Canceled {
		return nil
	}

	return err
}
