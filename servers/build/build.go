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
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/procfile"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/stackbuild"
	"miren.dev/runtime/pkg/tarx"
)

type Builder struct {
	Log       *slog.Logger
	EAS       *entityserver_v1alpha.EntityAccessClient
	ec        *entityserver.Client
	appClient *app.Client
	TempDir   string
	Registry  string

	Resolver netresolve.Resolver
}

func NewBuilder(log *slog.Logger, eas *entityserver_v1alpha.EntityAccessClient, appClient *app.Client, res netresolve.Resolver, tmpdir string) *Builder {
	return &Builder{
		Log:       log.With("module", "builder"),
		EAS:       eas,
		appClient: appClient,
		Resolver:  res,
		TempDir:   tmpdir,
		ec:        entityserver.NewClient(log, eas),
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
	dr, err := dfs.Open(appconfig.AppConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File not found is expected for apps without app.toml
			return nil, nil
		}
		// Return other errors (permission denied, IO errors, etc.)
		return nil, err
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

// sendErrorStatus sends an error status update if status is not nil, logging any send errors
func (b *Builder) sendErrorStatus(ctx context.Context, status *stream.SendStreamClient[*build_v1alpha.Status], format string, args ...interface{}) {
	if status != nil {
		so := new(build_v1alpha.Status)
		so.Update().SetError(fmt.Sprintf(format, args...))
		if _, err := status.Send(ctx, so); err != nil {
			b.Log.Warn("error sending error status", "error", err)
		}
	}
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
		_, _ = status.Send(ctx, so)
	}

	b.Log.Debug("receiving tar data", "app", name, "tempdir", path)

	r := stream.ToReader(ctx, td)

	tr, err := tarx.TarFS(r, path)
	if err != nil {
		b.sendErrorStatus(ctx, status, "Error untaring data: %v", err)
		return fmt.Errorf("error untaring data: %w", err)
	}

	if status != nil {
		so.Update().SetMessage("Launching builder")
		_, _ = status.Send(ctx, so)
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
		dr, err := tr.Open("Dockerfile.miren")
		if err == nil {
			buildStack.Stack = "dockerfile"
			buildStack.Input = "Dockerfile.miren"
			dr.Close()
		} else {
			buildStack.Stack = "auto"
		}
	}

	// Check if stack is supported before launching buildkit
	if buildStack.Stack == "auto" {
		_, err := stackbuild.DetectStack(buildStack.CodeDir)
		if err != nil {
			b.Log.Error("stack detection failed", "error", err, "app", name, "codeDir", buildStack.CodeDir)
			b.sendErrorStatus(ctx, status, "No supported stack detected for app %s: %v", name, err)
			return fmt.Errorf("no supported stack detected for app %s: %w", name, err)
		}
		b.Log.Debug("stack detection successful, proceeding with build")
	}

	// Now we know the stack is valid, proceed with buildkit setup
	b.Log.Debug("setting up buildkit")

	cacheDir := filepath.Join(b.TempDir, "buildkit-cache")
	b.Log.Debug("creating buildkit cache directory", "path", cacheDir)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		b.Log.Error("failed to create buildkit cache directory", "error", err, "path", cacheDir)
		b.sendErrorStatus(ctx, status, "Failed to create buildkit cache directory: %v", err)
		return fmt.Errorf("failed to create buildkit cache directory: %w", err)
	}

	lbk := &LaunchBuildkit{
		log: b.Log.With("module", "launchbuildkit"),
		eac: b.EAS,
	}
	b.Log.Debug("created LaunchBuildkit instance")

	b.Log.Debug("resolving cluster.local for buildkit")
	ip, err := b.Resolver.LookupHost("cluster.local")
	if err != nil {
		b.Log.Error("failed to resolve cluster.local", "error", err)
		b.sendErrorStatus(ctx, status, "Error resolving cluster.local: %v", err)
		return fmt.Errorf("error resolving cluster.local: %w", err)
	}
	b.Log.Debug("resolved cluster.local", "ip", ip.String())

	b.Log.Info("starting buildkit launch", "clusterIP", ip.String(), "logEntity", name)
	rbk, err := lbk.Launch(ctx, ip.String(), WithLogEntity(name), WithLogAttrs(map[string]string{
		"version": "build",
	}))
	if err != nil {
		b.Log.Error("failed to launch buildkit", "error", err)
		b.sendErrorStatus(ctx, status, "Failed to launch buildkit: %v", err)
		return err
	}
	b.Log.Info("buildkit launch completed successfully")

	defer func() {
		if err := rbk.Close(ctx); err != nil {
			b.Log.Error("failed to close buildkit", "error", err)
		}
	}()

	b.Log.Debug("attempting to get buildkit client")
	bkc, err := rbk.Client(ctx)
	if err != nil {
		b.Log.Error("failed to get buildkit client", "error", err)
		b.sendErrorStatus(ctx, status, "Failed to get buildkit client: %v", err)
		return err
	}
	b.Log.Debug("successfully obtained buildkit client")

	defer bkc.Close()

	b.Log.Debug("getting buildkit daemon info")
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
		b.sendErrorStatus(ctx, status, "Error getting next version: %v", err)
		return err
	}

	var tos []TransformOptions

	tos = append(tos,
		WithCacheDir(cacheDir),
		WithBuildArg("MIREN_VERSION", mrv.Version),
	)

	if status != nil {
		tos = append(tos, WithPhaseUpdates(func(phase string) {
			switch phase {
			case "export":
				so.Update().SetMessage("Registering image")
				_, _ = status.Send(ctx, so)
			case "solving":
				so.Update().SetMessage("Calculating build")
				_, _ = status.Send(ctx, so)
			case "solved":
				so.Update().SetMessage("Building image")
				_, _ = status.Send(ctx, so)
			default:
				so.Update().SetMessage(phase)
				_, _ = status.Send(ctx, so)
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
		_, _ = status.Send(ctx, so)
	}

	imgName := mrv.ImageUrl

	res, err := bk.BuildImage(ctx, tr, buildStack, name, imgName, tos...)
	if err != nil {
		b.Log.Error("error building image", "error", err)
		b.sendErrorStatus(ctx, status, "Error building image: %v", err)
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
		// If the appconfig contains any commands, extract those
		for k, v := range ac.Services {
			if v != nil && v.Command != "" {
				srvMap[k] = v.Command
			}
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

	// Prioritize the app config over the Procfile; if a service is defined in both, use the app config
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

	// Collect all service names
	allServiceNames := make([]string, 0, len(srvMap))
	for serviceName := range srvMap {
		allServiceNames = append(allServiceNames, serviceName)
	}

	// Resolve defaults for all services
	if ac != nil {
		ac.ResolveDefaults(allServiceNames)
	} else {
		// No app.toml - create minimal config with defaults
		ac = &appconfig.AppConfig{}
		ac.ResolveDefaults(allServiceNames)
	}

	// Build Config.Services[] from fully-resolved appconfig
	mrv.Config.Services = nil
	for serviceName := range srvMap {
		svc := core_v1alpha.Services{
			Name: serviceName,
		}

		// Map from appconfig to entity schema
		// After ResolveDefaults(), every service is guaranteed to have config
		if serviceConfig, ok := ac.Services[serviceName]; ok && serviceConfig.Concurrency != nil {
			svc.ServiceConcurrency = core_v1alpha.ServiceConcurrency{
				Mode:                serviceConfig.Concurrency.Mode,
				NumInstances:        int64(serviceConfig.Concurrency.NumInstances),
				RequestsPerInstance: int64(serviceConfig.Concurrency.RequestsPerInstance),
				ScaleDownDelay:      serviceConfig.Concurrency.ScaleDownDelay,
			}
		}

		mrv.Config.Services = append(mrv.Config.Services, svc)
	}

	id, err := b.ec.Create(ctx, mrv.Version, mrv)
	if err != nil {
		return fmt.Errorf("error creating app version: %w", err)
	}

	// Remember the old version before updating
	oldVersion := appRec.ActiveVersion

	b.Log.Info("updating app entity with new version", "app", name, "version", mrv.Version)
	err = b.appClient.SetActiveVersion(ctx, name, string(id))
	if err != nil {
		return fmt.Errorf("error updating app entity: %w", err)
	}

	b.Log.Info("app version updated", "app", name, "version", mrv.Version)

	// Scale down old version pools (immutable pool approach)
	if oldVersion != "" {
		b.Log.Info("scaling down old version pools", "oldVersion", oldVersion)

		// Query for all SandboxPool entities
		pools, err := b.ec.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandboxPool))
		if err != nil {
			b.Log.Error("error listing sandbox pools", "error", err)
			// Don't fail the build if we can't scale down old pools
		} else {
			for pools.Next() {
				var pool compute.SandboxPool
				err := pools.Read(&pool)
				if err != nil {
					b.Log.Error("error reading sandbox pool", "error", err)
					continue
				}

				// Only scale down pools that reference the old version
				if pool.SandboxSpec.Version == oldVersion {
					b.Log.Info("scaling down old pool", "pool", pool.ID, "service", pool.Service)

					// Set desired_instances to 0 explicitly; Patch preserves zero values
					attrs := []entity.Attr{
						entity.Ref(entity.DBId, pool.ID),
						entity.Int64(compute.SandboxPoolDesiredInstancesId, 0),
					}
					_, err = b.EAS.Patch(ctx, attrs, 0)
					if err != nil {
						b.Log.Error("error scaling down old pool", "pool", pool.ID, "error", err)
						continue
					}

					b.Log.Info("old pool scaled to zero", "pool", pool.ID, "service", pool.Service)
				}
			}
		}
	}

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
