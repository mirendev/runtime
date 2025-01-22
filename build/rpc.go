package build

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/mr-tron/base58"
	"miren.dev/runtime/app"
	"miren.dev/runtime/build/launch"
	"miren.dev/runtime/image"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rpc/stream"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg build -input build.yml -output rpc.gen.go

type RPCBuilder struct {
	Log     *slog.Logger
	LBK     *launch.LaunchBuildkit
	TempDir string `asm:"tempdir"`

	AppAccess      *app.AppAccess
	ImportImporter *image.ImageImporter
	LogWriter      observability.LogWriter
}

func (b *RPCBuilder) nextVersion(ctx context.Context, name string) (string, error) {
	ac, err := b.AppAccess.LoadApp(ctx, name)
	if err != nil {
		return "", err
	}

	data := make([]byte, 16)
	_, err = rand.Read(data)
	if err != nil {
		return "", err
	}

	ver := name + "-" + base58.Encode(data)

	err = b.AppAccess.CreateVersion(ctx, &app.AppVersion{
		AppId:   ac.Id,
		Version: ver,
	})
	if err != nil {
		return "", err
	}

	return ver, nil
}

func (b *RPCBuilder) BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error {
	args := state.Args()

	name := args.Application()
	td := args.Tardata()

	path, err := os.MkdirTemp(b.TempDir, "buildkit-")
	if err != nil {
		return err
	}

	b.Log.Debug("receiving tar data", "app", name, "tempdir", path)

	r := stream.ToReader(ctx, td)

	tr, err := TarFS(r, path)
	if err != nil {
		return fmt.Errorf("error untaring data: %w", err)
	}

	b.Log.Debug("launching buildkitd")
	rbk, err := b.LBK.Launch(ctx)
	if err != nil {
		return err
	}

	defer rbk.Close(context.Background())

	bkc, err := rbk.Client(ctx)
	if err != nil {
		return err
	}

	defer bkc.Close()

	ci, err := bkc.Info(ctx)
	if err != nil {
		b.Log.Error("error getting buildkid info", "error", err)
	} else {
		spew.Dump(ci.BuildkitVersion)
	}

	bk := &Buildkit{
		Client:    bkc,
		Log:       b.Log,
		LogWriter: b.LogWriter,
	}
	if err != nil {
		return err
	}

	mrv, err := b.nextVersion(ctx, name)
	if err != nil {
		return err
	}

	o, err := bk.Transform(ctx, tr)
	if err != nil {
		return err
	}

	b.Log.Debug("importing tar into image", "app", name, "version", mrv)

	err = b.ImportImporter.ImportImage(ctx, o, name+":"+mrv)
	if err != nil {
		return err
	}

	state.Results().SetVersion(mrv)

	return nil
}
