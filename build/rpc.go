package build

import (
	"context"
	"crypto/rand"

	"github.com/mr-tron/base58"
	"miren.dev/runtime/app"
	"miren.dev/runtime/image"
	"miren.dev/runtime/pkg/rpc/stream"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg build -input build.yml -output rpc.gen.go

type RPCBuilder struct {
	BuildKit *Buildkit
	TempDir  string `name:"tempdir"`

	AppAccess      *app.AppAccess
	ImportImporter *image.ImageImporter
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

	r := stream.ToReader(ctx, td)

	tr, err := TarFS(r, b.TempDir)
	if err != nil {
		return err
	}

	o, err := b.BuildKit.Transform(ctx, tr)
	if err != nil {
		return err
	}

	mrv, err := b.nextVersion(ctx, name)
	if err != nil {
		return err
	}

	err = b.ImportImporter.ImportImage(ctx, o, mrv)
	if err != nil {
		return err
	}

	state.Results().SetVersion(mrv)

	return nil
}
