package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"miren.dev/runtime/lsvd/lve/paths"
	"miren.dev/runtime/lsvd/lve/pkg/id"
	"miren.dev/runtime/pkg/units"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/lab47/cleo"
	"github.com/lima-vm/go-qcow2reader"
	"github.com/mitchellh/cli"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/lsvd/pkg/nbd"
)

type CLI struct {
	log *slog.Logger

	lc *cli.CLI
}

type Global struct {
	Config string `short:"c" long:"config" description:"storage configuration" required:"true"`
	Debug  bool   `short:"D" long:"debug" description:"enabel debug mode"`
}

func NewCLI(log *slog.Logger, args []string) (*CLI, error) {
	c := &CLI{
		log: log,
		lc:  cli.NewCLI("lsvd", "alpha"),
	}

	c.lc.Args = args

	err := c.setupCommands()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *CLI) Run() (int, error) {
	return c.lc.Run()
}

func (c *CLI) setupCommands() error {
	c.lc.Commands = map[string]cli.CommandFactory{
		"volume list": func() (cli.Command, error) {
			return cleo.Infer("volume list", "list all volumes", c.volumeList), nil
		},
		"volume init": func() (cli.Command, error) {
			return cleo.Infer("volume init", "initialize a volume", c.volumeInit), nil
		},
		"volume inspect": func() (cli.Command, error) {
			return cleo.Infer("volume inspect", "inspect a volume", c.volumeInspect), nil
		},
		"volume pack": func() (cli.Command, error) {
			return cleo.Infer("volume pack", "repack a volume", c.volumePack), nil
		},
		"nbd": func() (cli.Command, error) {
			return cleo.Infer("nbd", "service a volume over nbd", c.nbdServe), nil
		},
		"dd": func() (cli.Command, error) {
			return cleo.Infer("dd", "provide raw access to a lsvd disk", c.dd), nil
		},
		"sha256": func() (cli.Command, error) {
			return cleo.Infer("sha256", "hash the contents of the volume", c.sha256), nil
		},
	}

	return nil
}

func (c *CLI) loadSegmentAccess(ctx context.Context, path string) (lsvd.SegmentAccess, error) {
	cfg, err := lsvd.LoadConfig(path)
	if err != nil {
		c.log.Error("error loading configuration", "error", err)
		os.Exit(1)
	}

	var sa lsvd.SegmentAccess

	if cfg.Storage.FilePath != "" {
		if cfg.Storage.S3.Bucket != "" {
			c.log.Error("storage is either filepath, or s3, not both")
			os.Exit(1)
		}

		storagePath, err := filepath.Abs(cfg.Storage.FilePath)
		if err != nil {
			c.log.Error("error resolving file path to store objects", "error", err)
			os.Exit(1)
		}

		sa = &lsvd.LocalFileAccess{Dir: storagePath, Log: c.log}
	} else if cfg.Storage.S3.Bucket != "" {
		awsCfg, err := config.LoadDefaultConfig(ctx, func(lo *config.LoadOptions) error {
			lo.Region = cfg.Storage.S3.Region

			if cfg.Storage.S3.AccessKey != "" {
				lo.Credentials = credentials.NewStaticCredentialsProvider(
					cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey, "",
				)
			}
			return nil
		})
		if err != nil {
			c.log.Error("error initializing S3 configuration", "error", err)
			os.Exit(1)
		}

		sa, err = lsvd.NewS3Access(c.log, cfg.Storage.S3.URL, cfg.Storage.S3.Bucket, awsCfg)
		if err != nil {
			c.log.Error("error initializing S3 access", "error", err)
			os.Exit(1)
		}
	} else {
		return nil, fmt.Errorf("no proper storage backend defined")
	}

	return sa, nil
}

func (c *CLI) volumeList(ctx context.Context, opts struct {
	Global
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	volumes, err := sa.ListVolumes(ctx)
	if err != nil {
		c.log.Error("error listing volumes", "error", err)
		os.Exit(1)
	}

	tr := tabwriter.NewWriter(os.Stdout, 2, 2, 1, ' ', 0)
	defer tr.Flush()

	fmt.Fprintf(tr, "NAME\tSIZE\n")

	for _, vol := range volumes {
		vi, err := sa.GetVolumeInfo(ctx, vol)
		if err != nil {
			return err
		}
		fmt.Fprintf(tr, "%s\t%s\n", vol, vi.Size.Short())
	}

	return nil
}

const (
	kilo = 1000
	mega = kilo * 1000
	giga = mega * 1000
	tera = giga * 1000
	peta = tera * 1000
)

var sizeSuffix = map[string]int{
	"k": kilo,
	"K": kilo,
	"m": mega,
	"M": mega,
	"g": giga,
	"G": giga,
	"t": tera,
	"T": tera,
	"p": peta,
	"P": peta,
}

func niceSize(sz int64) string {
	cases := []struct {
		f float64
		s string
	}{
		{peta, "PB"},
		{tera, "TB"},
		{giga, "GB"},
		{mega, "MB"},
		{kilo, "KB"},
	}

	x := float64(sz)

	for _, c := range cases {
		sub := x / c.f
		if sub >= 1.0 {
			return fmt.Sprintf("%.3f%s", sub, c.s)
		}
	}

	return fmt.Sprintf("%db", sz)
}

func (c *CLI) volumeInit(ctx context.Context, opts struct {
	Global
	Name   string `short:"n" long:"name" description:"name of volume to create" required:"true"`
	Size   string `short:"s" long:"size" description:"size to advertise the volume as" required:"true"`
	Parent string `short:"p" long:"parent" description:"parent volume to pull blocks from"`
	UUID   string `short:"u" long:"uuid" description:"UUID identifier for the volume"`
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	var size int64

	for suf, factor := range sizeSuffix {
		if strings.HasSuffix(opts.Size, suf) {
			base, err := strconv.ParseInt(opts.Size[:len(opts.Size)-len(suf)], 10, 64)
			if err != nil {
				return errors.Wrapf(err, "parsing size")
			}

			size = base * int64(factor)
		}
	}

	if opts.Name == "" {
		return fmt.Errorf("name must not be empty")
	}

	if opts.UUID == "" {
		opts.UUID = uuid.NewString()
	}

	var parentInfo *lsvd.VolumeInfo

	if opts.Parent != "" {
		parentInfo, err = sa.GetVolumeInfo(ctx, opts.Parent)
		if err != nil {
			return fmt.Errorf("error loading parent volume: %s", err)
		}
	}

	err = sa.InitVolume(ctx, &lsvd.VolumeInfo{
		Name:   opts.Name,
		Size:   units.Bytes(size),
		Parent: opts.Parent,
		UUID:   opts.UUID,
	})
	if err != nil {
		c.log.Error("error listing volumes", "error", err)
		os.Exit(1)
	}

	fmt.Printf("volume '%s' created (%d bytes, id: %s)\n", opts.Name, size, opts.UUID)

	if parentInfo != nil {
		fmt.Printf("  parent: %s (id: %s\n", opts.Parent, parentInfo.UUID)
	}

	return nil
}

func (c *CLI) volumeInspect(ctx context.Context, opts struct {
	Global
	Name string `short:"n" long:"name" description:"name of volume to create" required:"true"`
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	info, err := sa.GetVolumeInfo(ctx, opts.Name)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %d (id: %s)\n", info.Name, info.Size, info.UUID)
	if info.Parent != "" {
		fmt.Printf("  parent: %s\n", info.Parent)
	}

	vol, err := sa.OpenVolume(ctx, opts.Name)
	if err != nil {
		return err
	}

	entries, err := vol.ListSegments(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("%d segments\n", len(entries))

	for _, ent := range entries {
		fmt.Printf("  %s\n", ent)
	}

	return nil
}

func (c *CLI) volumePack(ctx context.Context, opts struct {
	Global
	Name string `short:"n" long:"name" description:"name of volume to create" required:"true"`
	Path string `short:"p" long:"path" description:"path for cached data" required:"true"`
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	log := c.log

	d, err := lsvd.NewDisk(ctx, log, opts.Path,
		lsvd.WithSegmentAccess(sa),
		lsvd.WithVolumeName(opts.Name),
	)
	if err != nil {
		log.Error("error creating new disk", "error", err)
		os.Exit(1)
	}

	info, err := sa.GetVolumeInfo(ctx, opts.Name)
	if err != nil {
		return err
	}

	log.Info("Packing volume", "size", info.Size)

	err = d.Pack(ctx)
	if err != nil {
		return err
	}

	info, err = sa.GetVolumeInfo(ctx, opts.Name)
	if err != nil {
		return err
	}

	log.Info("Packing volume complete", "new-size", info.Size)

	vol, err := sa.OpenVolume(ctx, opts.Name)
	if err != nil {
		return err
	}

	entries, err := vol.ListSegments(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("%d segments\n", len(entries))

	for _, ent := range entries {
		fmt.Printf("  %s\n", ent)
	}

	return nil
}

func (c *CLI) nbdServe(ctx context.Context, opts struct {
	Global
	Name        string `short:"n" long:"name" description:"name of volume to serve"`
	Path        string `short:"p" long:"path" description:"path for cached data or lve root if id" required:"true"`
	Addr        string `short:"a" long:"addr" default:":8989" description:"address to listen on"`
	MetricsAddr string `long:"metrics" default:":2121" description:"address to expose metrics on"`
	Id          id.Id  `short:"i" long:"id" description:"identifier of disk"`
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	log := c.log
	path := opts.Path
	name := opts.Name
	addr := opts.Addr

	var diskPath, parentPath string

	if opts.Id.Valid() {
		name = opts.Id.String()

		root := paths.Root(path)

		err := root.SetupDisk(opts.Id)
		if err != nil {
			return err
		}

		addr = "unix:" + root.DiskSocket(opts.Id)

		diskPath = root.DiskData(opts.Id, "cache")
		parentPath = root.DiskData(opts.Id, "parent-cache")
	} else {
		if name == "" {
			name = "default"
		}

		diskPath = path
		parentPath = filepath.Join(path, "parent")
	}

	vol, err := sa.GetVolumeInfo(ctx, name)
	if err != nil {
		return fmt.Errorf("unable to locate volume: %s", err)
	}

	diskOpts := []lsvd.Option{
		lsvd.WithSegmentAccess(sa),
		lsvd.WithVolumeName(name),
		lsvd.EnableAutoGC,
	}

	if vol.Parent != "" {
		pvol, err := sa.GetVolumeInfo(ctx, vol.Parent)
		if err != nil {
			return fmt.Errorf("unable to locate volume: %s", err)
		}

		log.Info("using parent volume", "name", vol.Parent, "uuid", pvol.UUID)

		err = os.MkdirAll(parentPath, 0755)
		if err != nil {
			return errors.Wrapf(err, "creating parent-cache dir")
		}

		lower, err := lsvd.NewDisk(ctx, log,
			parentPath,
			lsvd.WithSegmentAccess(sa),
			lsvd.WithVolumeName(vol.Parent),
			lsvd.ReadOnly(),
		)
		if err != nil {
			log.Error("error creating new disk", "error", err)
			os.Exit(1)
		}

		diskOpts = append(diskOpts,
			lsvd.WithLowerLayer(lower),
		)
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

	var l net.Listener

	if strings.HasPrefix(addr, "unix:") {
		l, err = net.Listen("unix", addr[5:])
		if err != nil {
			log.Error("error listening on addr", "error", err, "addr", opts.Addr)
			os.Exit(1)
		}
	} else {
		l, err = net.Listen("tcp", addr)
		if err != nil {
			log.Error("error listening on addr", "error", err, "addr", opts.Addr)
			os.Exit(1)
		}
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		l.Close()
	}()

	exports := []*nbd.Export{
		{
			Name:        name,
			Description: "disk",
			Backend:     lsvd.NBDWrapper(ctx, log, d),
		},
	}

	log.Info("listening for connections", "addr", opts.Addr)

	nbdOpts := &nbd.Options{
		MinimumBlockSize:   4096,
		PreferredBlockSize: 4096,
		MaximumBlockSize:   4096,
	}

	http.Handle("/metrics", promhttp.Handler())
	// Will also include pprof via the init() in net/http/pprof
	go http.ListenAndServe(opts.MetricsAddr, nil)

	if url := os.Getenv("NATS_URL"); url != "" {
		nc, err := lsvd.NewNATSConnector(log, d, url, name)
		if err != nil {
			log.Error("error creating nats connector", "error", err, "url", url)
			os.Exit(1)
		}

		err = nc.Start(ctx)
		if err != nil {
			log.Error("error starting nats connector", "error", err, "url", url)
			os.Exit(1)
		}

		log.Info("connected disk to nats", "url", url)
	}

	for {
		c, err := l.Accept()
		if err != nil {
			break
		}

		log.Info("connection to nbd server", "remote", c.RemoteAddr().String())

		err = nbd.Handle(log, c, exports, nbdOpts)
		if err != nil {
			log.Error("error handling nbd client", "error", err)
		}
	}

	return nil
}

func (c *CLI) dd(ctx context.Context, opts struct {
	Global
	Name     string `short:"n" long:"name" description:"name of volume access" required:"true"`
	Path     string `short:"p" long:"path" description:"path for cached data" required:"true"`
	Input    string `short:"i" long:"input" description:"url to populate the disk from"`
	BS       int    `long:"bs" description:"number of blocks to make the extents (default 20)"`
	Expand   bool   `long:"expand" description:"expand compressed files (like qcow2)"`
	Verify   string `long:"verify" description:"sha256 of the data to check it against"`
	Readback bool   `long:"readback" description:"after importing, read back the data to validate it"`
}) error {
	sa, err := c.loadSegmentAccess(ctx, opts.Config)
	if err != nil {
		return err
	}

	log := c.log
	path := opts.Path
	name := opts.Name

	var verify []byte
	if opts.Verify != "" {
		verify, err = hex.DecodeString(opts.Verify)
		if err != nil {
			log.Error("error parsing verify sha256")
			os.Exit(1)
		}

		log.Info("expected sum of data", "sum", hex.EncodeToString(verify))
	}

	d, err := lsvd.NewDisk(ctx, log, path,
		lsvd.WithSegmentAccess(sa),
		lsvd.WithVolumeName(name),
	)
	if err != nil {
		log.Error("error creating new disk", "error", err)
		os.Exit(1)
	}

	defer d.Close(ctx)

	var reader io.Reader

	if f, err := os.Open(opts.Input); err == nil {
		defer f.Close()

		if opts.Expand {
			img, err := qcow2reader.Open(f)
			if err != nil {
				log.Error("error opening qcow2 file", "error", err)
				os.Exit(1)
			}

			log.Info("detected file as qcow2 format")

			reader = io.NewSectionReader(img, 0, img.Size())
		} else {
			log.Info("detected file as raw format")
			reader = f
		}
	} else {
		resp, err := http.Get(opts.Input)
		if err != nil {
			d.Close(ctx)
			log.Error("error fetching url", "error", err)
			os.Exit(1)
		}

		defer resp.Body.Close()

		reader = resp.Body
	}

	bs := opts.BS
	if bs == 0 {
		bs = 32 // 128k
	}

	h := sha256.New()

	buf := make([]byte, lsvd.BlockSize*bs)

	br := bufio.NewReader(reader)

	extent := lsvd.Extent{Blocks: uint32(bs)}

	input := io.TeeReader(br, h)

	var total, extents int
	for {
		n, err := io.ReadFull(input, buf)
		if n == 0 && err != nil {
			break
		}

		extents++
		total += n

		data := buf

		err = d.WriteExtent(ctx, lsvd.MapRangeData(extent, data))
		if err != nil {
			d.Close(ctx)
			log.Error("error writing data", "error", err, "extent", extent)
			os.Exit(1)
		}

		extent.LBA += lsvd.LBA(bs)
	}

	sum := h.Sum(nil)

	if len(verify) > 0 {
		if bytes.Equal(verify, sum) {
			log.Info("data imported and verified",
				"extents-sent", extents, "extents-live", d.Extents(),
				"size", total, "sha256", hex.EncodeToString(sum))

		} else {
			log.Error("data imported and failed verification", "size", total,
				"sha256", hex.EncodeToString(sum),
				"expected", hex.EncodeToString(verify),
			)

			return nil
		}
	} else {
		log.Info("data imported",
			"extents-sent", extents, "extents-live", d.Extents(),
			"size", total, "sha256", hex.EncodeToString(sum))
	}

	if opts.Readback {
		log.Info("reading data back to validate", "size", total)

		h := sha256.New()

		extent := lsvd.Extent{
			Blocks: uint32(bs),
		}

		// shift the block size so we force unalligned extent reads and validate more
		// of the read machinery.
		empty := make([]byte, lsvd.BlockSize*bs)

		left := total

		ctx := lsvd.NewContext(ctx)

		start := time.Now()
		marker := ctx.Marker()

		for left > 0 {
			ctx.ResetTo(marker)

			data, err := d.ReadExtent(ctx, extent)
			if err != nil {
				log.Error("error reading data", "error", err)
				os.Exit(1)
			}

			extent.LBA += lsvd.LBA(bs)

			var b []byte

			if data.EmptyP() {
				b = empty
			} else {
				b = data.ReadData()
			}

			if left < len(b) {
				b = b[:left]
			}

			total += len(b)
			left -= len(b)

			h.Write(b)
		}

		diff := time.Since(start)

		mbPerSec := (float64(total) / (1024 * 1024)) / diff.Seconds()

		rbSum := h.Sum(nil)

		if !bytes.Equal(sum, rbSum) {
			log.Error("readback data validated validation", "size", total, "sum",
				hex.EncodeToString(sum), "expected", hex.EncodeToString(rbSum))
		} else {
			log.Info("data verified", "size", total, "sha256", hex.EncodeToString(h.Sum(nil)), "elapes", diff, "mb-per-sec", mbPerSec)
		}
	}

	return nil
}

func (c *CLI) sha256(gctx context.Context, opts struct {
	Global
	Name  string   `short:"n" long:"name" description:"name of volume access" required:"true"`
	Path  string   `short:"p" long:"path" description:"path for cached data" required:"true"`
	Size  int      `short:"s" description:"read up to this many bytes"`
	Count int      `long:"count" description:"how many chunks of size -s to read (default 1)"`
	Seek  lsvd.LBA `long:"seek" description:"start at the given LBA"`
	BS    int      `long:"bs" description:"how many blocks to read at a time (default 20)"`
}) error {
	sa, err := c.loadSegmentAccess(gctx, opts.Config)
	if err != nil {
		return err
	}

	log := c.log
	path := opts.Path
	name := opts.Name

	d, err := lsvd.NewDisk(gctx, log, path,
		lsvd.WithSegmentAccess(sa),
		lsvd.WithVolumeName(name),
		lsvd.ReadOnly(),
	)
	if err != nil {
		log.Error("error creating new disk", "error", err)
		os.Exit(1)
	}

	defer d.Close(gctx)

	bs := opts.BS
	if bs == 0 {
		bs = 20
	}

	h := sha256.New()

	extent := lsvd.Extent{
		LBA:    opts.Seek,
		Blocks: uint32(bs),
	}

	empty := make([]byte, lsvd.BlockSize*bs)

	var total, left int

	left = opts.Size

	if opts.Count > 1 {
		left *= opts.Count
	}

	log.Warn("reading total", "total", left)

	ctx := lsvd.NewContext(gctx)

	start := time.Now()
	for left > 0 {
		data, err := d.ReadExtent(ctx, extent)
		if err != nil {
			log.Error("error reading data", "error", err)
			os.Exit(1)
		}

		extent.LBA += lsvd.LBA(bs)

		var b []byte

		if data.EmptyP() {
			b = empty
		} else {
			b = data.ReadData()
		}

		if left < len(b) {
			b = b[:left]
		}

		total += len(b)
		left -= len(b)

		h.Write(b)
	}

	diff := time.Since(start)

	mbPerSec := (float64(total) / (1024 * 1024)) / diff.Seconds()

	log.Info("data hashed", "size", total, "sha256", hex.EncodeToString(h.Sum(nil)), "elapes", diff, "mb-per-sec", mbPerSec)

	return nil
}
