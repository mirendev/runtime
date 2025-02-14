package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/moby/buildkit/client"
	"miren.dev/runtime/app"
	"miren.dev/runtime/build/launch"
	"miren.dev/runtime/image"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg build -input build.yml -output rpc.gen.go

type ClearVersioner interface {
	ClearOldVersions(ctx context.Context, current *app.AppVersion) error
}

type RPCBuilder struct {
	Log     *slog.Logger
	LBK     *launch.LaunchBuildkit
	TempDir string `asm:"tempdir"`

	CV             ClearVersioner
	AppAccess      *app.AppAccess
	ImportImporter *image.ImageImporter
	ImagePruner    *image.ImagePruner
	LogWriter      observability.LogWriter
}

func (b *RPCBuilder) nextVersion(ctx context.Context, name string) (*app.AppVersion, error) {
	ac, err := b.AppAccess.LoadApp(ctx, name)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}

		ac = &app.AppConfig{
			Name: name,
		}

		err = b.AppAccess.CreateApp(ctx, ac)
		if err != nil {
			return nil, err
		}

		b.Log.Info("created new app while deploying", "app", name)

		ac, err = b.AppAccess.LoadApp(ctx, name)
		if err != nil {
			return nil, err
		}
	}

	var currentCfg *app.Configuration

	cur, err := b.AppAccess.MostRecentVersion(ctx, ac)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}

		cfg := app.DefaultConfiguration
		currentCfg = &cfg
	} else {
		currentCfg = cur.Configuration
	}

	ver := name + "-" + idgen.Gen("v")

	av := &app.AppVersion{
		App:     ac,
		AppId:   ac.Id,
		Version: ver,
		ImageId: ver,

		// We always port the current configuration forward. This means that
		// the application itself has no configuration, instead we've got a per
		// version configuration that can mutate each time.
		Configuration: currentCfg,
	}

	err = b.AppAccess.CreateVersion(ctx, av)
	if err != nil {
		return nil, err
	}

	return av, nil
}

func (b *RPCBuilder) BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error {
	args := state.Args()

	name := args.Application()
	td := args.Tardata()

	path, err := os.MkdirTemp(b.TempDir, "buildkit-")
	if err != nil {
		return err
	}

	defer os.RemoveAll(path)

	status := args.Status()

	so := new(Status)

	if status != nil {
		so.Update().SetMessage("Reading application data")
		status.Send(ctx, so)
	}

	b.Log.Debug("receiving tar data", "app", name, "tempdir", path)

	r := stream.ToReader(ctx, td)

	tr, err := TarFS(r, path)
	if err != nil {
		return fmt.Errorf("error untaring data: %w", err)
	}

	if status != nil {
		so.Update().SetMessage("Launching builder")
		status.Send(ctx, so)
	}

	b.Log.Debug("launching buildkitd")

	cacheDir := filepath.Join(b.TempDir, "buildkit-cache")
	os.MkdirAll(cacheDir, 0755)

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
		b.Log.Debug("buildkitd info", "version", ci.BuildkitVersion.Version, "rev", ci.BuildkitVersion.Revision)
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

	var tos []TransformOptions

	tos = append(tos,
		WithCacheDir(cacheDir),
		WithBuildArg("RUNTIME_VERSION", mrv.Version),
	)

	if status != nil {
		tos = append(tos, WithPhaseUpdates(func(phase string) {
			switch phase {
			case "export":
				so.Update().SetMessage("Registering image")
				status.Send(ctx, so)
			case "solved":
				so.Update().SetMessage("Building image")
				status.Send(ctx, so)
			}
		}))

		tos = append(tos, WithStatusUpdates(func(ss *client.SolveStatus, sj []byte) {
			so := new(Status)
			so.Update().SetBuildkit(sj)
			_, err := status.Send(ctx, so)
			if err != nil {
				b.Log.Warn("error sending status update", "error", err)
			}
		}))
	}

	if status != nil {
		so.Update().SetMessage("Calculating build")
		status.Send(ctx, so)
	}

	var importError error

	var wg sync.WaitGroup

	err = bk.BuildImage(ctx, tr, func() (io.WriteCloser, error) {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Log.Debug("importing tar into image", "app", name, "image", mrv.ImageName())
			importError = b.ImportImporter.ImportImage(ctx, r, mrv.ImageName())
			b.Log.Debug("finished importing image", "app", name, "image", mrv.ImageName(), "error", importError)
		}()

		return w, nil
	}, tos...)

	if err != nil {
		return err
	}

	wg.Wait()

	if importError != nil {
		b.Log.Debug("error importing image", "app", name, "image", mrv.ImageName(), "error", importError)
	}

	b.Log.Info("clearing old version", "app", name, "new-ver", mrv.Version)
	err = b.CV.ClearOldVersions(ctx, mrv)
	if err != nil {
		return err
	}

	err = b.ImagePruner.PruneApp(context.Background(), name)
	if err != nil {
		b.Log.Error("error pruning app images", "app", name, "error", err)
	}

	state.Results().SetVersion(mrv.Version)

	return nil
}
