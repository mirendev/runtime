package commands

import (
	"net/url"
	"strings"

	"github.com/google/uuid"
	"miren.dev/runtime/dataset"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/units"
)

func DiskCreate(ctx *Context, opts struct {
	Dir  string `short:"d" long:"dir" description:"Directory to create the disk in"`
	Name string `short:"n" long:"name" description:"Name of the disk"`
	Size string `short:"s" long:"size" description:"Size of the disk (use si units as suffix)"`

	DataSetURI string `long:"dataset" description:"Dataset URI"`
}) error {
	var sa lsvd.SegmentAccess

	if opts.DataSetURI == "" {
		sa = &lsvd.LocalFileAccess{Dir: opts.Dir, Log: ctx.Log}
	} else {
		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		if err != nil {
			return err
		}

		u, err := url.Parse(opts.DataSetURI)
		if err != nil {
			return err
		}

		client, err := cs.Connect(u.Host, strings.TrimPrefix(u.Path, "/"))
		if err != nil {
			return err
		}

		dc := &dataset.DataSetsClient{Client: client}
		sa = dataset.NewSegmentAccess(ctx.Log, dc, []string{"application/database"})
	}

	sz, err := units.ParseData(opts.Size)
	if err != nil {
		return err
	}

	vi, err := sa.GetVolumeInfo(ctx, opts.Name)
	if err == nil {
		ctx.Info("Disk already exists. name=%s, size=%s", opts.Name, vi.Size.Short())
		return nil
	}

	err = sa.InitVolume(ctx, &lsvd.VolumeInfo{
		Name: opts.Name,
		Size: sz.Bytes(),
		UUID: uuid.NewString(),
	})
	if err != nil {
		return err
	}

	ctx.Info("Disk created")

	return err
}
