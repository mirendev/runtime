package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/progress/progresswriter"

	"github.com/pkg/errors"
	"github.com/tonistiigi/fsutil"
)

type Buildkit struct {
	cl  *client.Client
	dir string
}

func NewBuildkit(ctx context.Context, cl *client.Client, tempdir string) (*Buildkit, error) {
	bk := &Buildkit{
		cl:  cl,
		dir: tempdir,
	}
	return bk, nil
}

func (b *Buildkit) Transform(ctx context.Context, r io.Reader) (io.Reader, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(gzr)

	dir, err := os.MkdirTemp(b.dir, "build")
	if err != nil {
		return nil, err
	}

	defer os.RemoveAll(dir)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		path := filepath.Join(dir, th.Name)
		if th.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path, 0755); err != nil {
				return nil, err
			}
		}

		if th.Typeflag == tar.TypeReg {
			f, err := os.Create(path)
			if err != nil {
				return nil, err
			}

			if _, err := io.Copy(f, tr); err != nil {
				return nil, err
			}
		}
	}

	mounts := map[string]fsutil.FS{}

	mounts["dockerfile"], err = fsutil.NewFS(dir)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	mounts["context"], err = fsutil.NewFS(dir)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	output := client.ExportEntry{
		Type: "oci",
		Output: func(attrs map[string]string) (io.WriteCloser, error) {
			return w, nil
		},
	}

	ref := identity.NewID()

	solveOpt := client.SolveOpt{
		Exports:     []client.ExportEntry{output},
		LocalMounts: mounts,
		Frontend:    "dockerfile.v0",
		Ref:         ref,
	}

	sreq := gateway.SolveRequest{
		Frontend:    solveOpt.Frontend,
		FrontendOpt: solveOpt.FrontendAttrs,
	}

	// not using shared context to not disrupt display but let is finish reporting errors
	pw, err := progresswriter.NewPrinter(ctx, os.Stderr, "plain")
	if err != nil {
		return nil, err
	}

	mw := progresswriter.NewMultiWriter(pw)

	_, err = b.cl.Build(ctx, solveOpt, "buildctl", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		res, err := c.Solve(ctx, sreq)
		if err != nil {
			return nil, err
		}

		return res, nil
	}, progresswriter.ResetTime(mw.WithPrefix("", false)).Status())
	if err != nil {
		return nil, err
	}

	return r, nil
}
