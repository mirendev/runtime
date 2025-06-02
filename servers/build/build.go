package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"

	"github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/defaults"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/procfile"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
)

type Builder struct {
	Log      *slog.Logger
	EAS      *entityserver_v1alpha.EntityAccessClient
	ec       *entityserver.Client
	TempDir  string
	Registry string

	Resolver netresolve.Resolver
}

func NewBuilder(log *slog.Logger, eas *entityserver_v1alpha.EntityAccessClient, res netresolve.Resolver, tmpdir string) *Builder {
	return &Builder{
		Log:      log.With("module", "builder"),
		EAS:      eas,
		Resolver: res,
		TempDir:  tmpdir,
		ec:       entityserver.NewClient(log, eas),
	}
}

func (b *Builder) nextVersion(ctx context.Context, name string) (
	*core_v1alpha.App,
	*core_v1alpha.AppVersion,
	string,
	error,
) {
	var appRec core_v1alpha.App

	err := b.ec.Get(ctx, name, &appRec)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil, "", err
		}

		appRec.Project = "project/default"

		id, err := b.ec.Create(ctx, name, &appRec)
		if err != nil {
			return nil, nil, "", err
		}
		appRec.ID = id
	}

	var currentCfg core_v1alpha.Config

	if appRec.ActiveVersion != "" {
		var verRec core_v1alpha.AppVersion

		err := b.ec.GetById(ctx, appRec.ActiveVersion, &verRec)
		if err != nil {
			return nil, nil, "", err
		}

		currentCfg = verRec.Config
	} else {
		currentCfg.Concurrency.Fixed = defaults.Concurrency
	}

	ver := name + "-" + idgen.Gen("v")
	art := name + "-" + idgen.Gen("a")

	b.Log.Info("creating new app version", "app", appRec.ID, "version", ver, "artifact", art)

	var av core_v1alpha.AppVersion
	av.App = appRec.ID
	av.Version = ver
	av.ImageUrl = "cluster.local:5000/" + name + ":" + art
	av.Config = currentCfg

	return &appRec, &av, art, nil
}

func (b *Builder) loadAppConfig(dfs fsutil.FS) (*appconfig.AppConfig, error) {
	dr, err := dfs.Open(".runtime/app.toml")
	if err != nil {
		return nil, nil
	}

	defer dr.Close()

	data, err := io.ReadAll(dr)
	if err != nil {
		return nil, err
	}

	ac, err := appconfig.Parse(data)
	if err != nil {
		return nil, err
	}

	return ac, nil
}

func (b *Builder) BuildFromTar(ctx context.Context, state *build_v1alpha.BuilderBuildFromTar) error {
	args := state.Args()

	name := args.Application()
	td := args.Tardata()

	path, err := os.MkdirTemp(b.TempDir, "buildkit-")
	if err != nil {
		return err
	}

	defer os.RemoveAll(path)

	status := args.Status()

	so := new(build_v1alpha.Status)

	if status != nil {
		so.Update().SetMessage("Reading application data")
		status.Send(ctx, so)
	}

	b.Log.Debug("receiving tar data", "app", name, "tempdir", path)

	r := stream.ToReader(ctx, td)

	tr, err := tarx.TarFS(r, path)
	if err != nil {
		return fmt.Errorf("error untaring data: %w", err)
	}

	if status != nil {
		so.Update().SetMessage("Launching builder")
		status.Send(ctx, so)
	}

	ac, err := b.loadAppConfig(tr)
	if err != nil {
		b.Log.Warn("error loading app config, ignoring", "error", err)
	}

	var buildStack BuildStack
	buildStack.CodeDir = path

	if ac != nil && ac.Build != nil {
		buildStack.OnBuild = ac.Build.OnBuild
		buildStack.Version = ac.Build.Version
		buildStack.AlpineImage = ac.Build.AlpineImage

		if ac.Build.Dockerfile != "" {
			buildStack.Stack = "dockerfile"
			buildStack.Input = ac.Build.Dockerfile

			b.Log.Info("using dockerfile from app config", "dockerfile", ac.Build.Dockerfile)
		}
	}

	if buildStack.Stack == "" {
		dr, err := tr.Open("Dockefile.runtime")
		if err == nil {
			buildStack.Stack = "dockerfile"
			buildStack.Input = "Dockerfile.runtime"
			dr.Close()
		} else {
			buildStack.Stack = "auto"
		}
	}

	b.Log.Debug("launching buildkitd")

	cacheDir := filepath.Join(b.TempDir, "buildkit-cache")
	os.MkdirAll(cacheDir, 0755)

	lbk := &LaunchBuildkit{
		log: b.Log.With("module", "launchbuildkit"),
		eac: b.EAS,
	}

	ip, err := b.Resolver.LookupHost("cluster.local")
	if err != nil {
		return fmt.Errorf("error resolving cluster.local: %w", err)
	}

	rbk, err := lbk.Launch(ctx, ip.String(), WithLogEntity(name), WithLogAttrs(map[string]string{
		"version": "build",
	}))
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
		Client: bkc,
		Log:    b.Log,
		//LogWriter: b.LogWriter,
	}

	appRec, mrv, artName, err := b.nextVersion(ctx, name)
	if err != nil {
		b.Log.Error("error getting next version", "error", err)
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
			case "solving":
				so.Update().SetMessage("Calculating build")
				status.Send(ctx, so)
			case "solved":
				so.Update().SetMessage("Building image")
				status.Send(ctx, so)
			default:
				so.Update().SetMessage(phase)
				status.Send(ctx, so)
			}
		}))

		tos = append(tos, WithStatusUpdates(func(ss *client.SolveStatus, sj []byte) {
			so := new(build_v1alpha.Status)
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

	imgName := mrv.ImageUrl

	res, err := bk.BuildImage(ctx, tr, buildStack, imgName, tos...)
	if err != nil {
		b.Log.Error("error building image", "error", err)
		return err
	}

	var artifact core_v1alpha.Artifact

	err = b.ec.Get(ctx, artName, &artifact)
	if err != nil {
		b.Log.Error("error creating artifact entity", "error", err)
		return fmt.Errorf("error creating artifact entity: %w", err)
	}

	b.Log.Debug("located stored artifact", "artifact", artifact.ID)

	mrv.Artifact = artifact.ID

	b.Log.Debug("build complete", "image", imgName)

	if res.Entrypoint != "" {
		mrv.Config.Entrypoint = res.Entrypoint
	}

	var serviceCmds []core_v1alpha.Commands

	srvMap := map[string]string{}

	if ac != nil {
		for k, v := range ac.Services {
			srvMap[k] = v
		}
	}

	services, err := b.readProcFile(tr)
	if err != nil {
		return fmt.Errorf("error reading procfile: %w", err)
	} else if services == nil {
		b.Log.Debug("no procfile found, using app config")
	} else {
		b.Log.Debug("using procfile", "services", maps.Keys(services))
	}

	// Prioritize the app config over the Procfile
	for k, v := range services {
		if _, ok := srvMap[k]; !ok {
			srvMap[k] = v
		}
	}

	for k, v := range srvMap {
		serviceCmds = append(serviceCmds, core_v1alpha.Commands{
			Service: k,
			Command: v,
		})
	}

	mrv.Config.Commands = serviceCmds

	id, err := b.ec.Create(ctx, mrv.Version, mrv)
	if err != nil {
		return fmt.Errorf("error creating app version: %w", err)
	}

	appRec.ActiveVersion = id

	/*
		var rpcE entityserver_v1alpha.Entity

		rpcE.SetAttrs(entity.Attrs(
			(&core_v1alpha.Metadata{
				Name: mrv.Version,
			}).Encode,
			entity.Ident, "app_verison/"+mrv.Version,
			mrv.Encode,
		))

		pr, err := b.EAS.Put(ctx, &rpcE)
		if err != nil {
			return err
		}

		appRec.ActiveVersion = entity.Id(pr.Id())
	*/

	err = b.ec.Update(ctx, appRec)
	if err != nil {
		return fmt.Errorf("error updating app entity: %w", err)
	}

	b.Log.Info("app version updated", "app", name, "version", mrv.Version)

	/*

		b.Log.Info("clearing old version", "app", name, "new-ver", mrv.Version)
		err = b.CV.ClearOldVersions(ctx, mrv)
		if err != nil {
			return err
		}

		err = b.ImagePruner.PruneApp(context.Background(), name)
		if err != nil {
			b.Log.Error("error pruning app images", "app", name, "error", err)
		}
	*/

	state.Results().SetVersion(mrv.Version)

	return nil
}

func (b *Builder) readProcFile(dfs fsutil.FS) (map[string]string, error) {
	r, err := dfs.Open("Procfile")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return procfile.Parser(data)
}
