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
	"slices"
	"strings"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/procfile"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/stackbuild"
	"miren.dev/runtime/pkg/tarx"
)

type Builder struct {
	Log           *slog.Logger
	EAS           *entityserver_v1alpha.EntityAccessClient
	ec            *entityserver.Client
	appClient     *app.Client
	ingressClient *ingress.Client
	TempDir       string
	Registry      string
	DNSHostname   string // Cloud-provisioned DNS hostname for default route display

	Resolver  netresolve.Resolver
	LogWriter observability.LogWriter
}

func NewBuilder(log *slog.Logger, eas *entityserver_v1alpha.EntityAccessClient, appClient *app.Client, res netresolve.Resolver, tmpdir string, logWriter observability.LogWriter, dnsHostname string) *Builder {
	return &Builder{
		Log:           log.With("module", "builder"),
		EAS:           eas,
		appClient:     appClient,
		ingressClient: ingress.NewClient(log, eas),
		Resolver:      res,
		TempDir:       tmpdir,
		ec:            entityserver.NewClient(log, eas),
		LogWriter:     logWriter,
		DNSHostname:   dnsHostname,
	}
}

// mergeServiceEnvVars merges per-service environment variables from app.toml into existing service env vars.
// Uses the same source-tracking logic as global variables:
// - Manual vars (source="manual") always persist and shadow config vars with the same key
// - app.toml vars (source="config") override existing config vars but never manual vars
// - Removing a var from app.toml only deletes it if source="config"
func mergeServiceEnvVars(existingEnvs []core_v1alpha.Env, newEnvs []core_v1alpha.Env) []core_v1alpha.Env {
	// If no new env vars from app.toml, preserve all existing
	if len(newEnvs) == 0 {
		return existingEnvs
	}

	// Build map of app.toml env vars
	newEnvMap := make(map[string]core_v1alpha.Env)
	for _, e := range newEnvs {
		newEnvMap[e.Key] = e
	}

	// Build result by merging
	envMap := make(map[string]core_v1alpha.Env)

	// Keep manual vars - they shadow config vars with the same key
	for _, e := range existingEnvs {
		source := e.Source
		if source == "" {
			source = "config" // backward compatibility
		}

		if source == "manual" {
			envMap[e.Key] = e
		}
		// config vars only kept if still in app.toml (checked below)
	}

	// Add app.toml vars, but never override manual vars
	for key, e := range newEnvMap {
		if _, hasManual := envMap[key]; !hasManual {
			envMap[key] = e
		}
	}

	// Convert back to slice
	result := make([]core_v1alpha.Env, 0, len(envMap))
	for _, e := range envMap {
		result = append(result, e)
	}

	return result
}

// buildServicesConfig collects services from app config and procfile,
// resolves defaults, and returns the final service configurations.
// This is the core logic for determining which services exist in an app_version
// and what their concurrency settings should be.
func buildServicesConfig(appConfig *appconfig.AppConfig, procfileServices map[string]string) []core_v1alpha.Services {
	// Build command map from app config
	srvMap := map[string]string{}
	if appConfig != nil {
		for k, v := range appConfig.Services {
			if v != nil && v.Command != "" {
				srvMap[k] = v.Command
			}
		}
	}

	// Add procfile services (app config takes precedence)
	for k, v := range procfileServices {
		if _, ok := srvMap[k]; !ok {
			srvMap[k] = v
		}
	}

	// Collect all service names from both commands and app config
	// Services may have concurrency config without explicit commands
	allServiceNames := make([]string, 0, len(srvMap))
	for serviceName := range srvMap {
		allServiceNames = append(allServiceNames, serviceName)
	}

	// Also include services that have config in app.toml but no commands
	if appConfig != nil {
		for serviceName := range appConfig.Services {
			if !slices.Contains(allServiceNames, serviceName) {
				allServiceNames = append(allServiceNames, serviceName)
			}
		}
	}

	// Resolve defaults for all services
	ac := appConfig
	if ac != nil {
		ac.ResolveDefaults(allServiceNames)
	} else {
		// No app.toml - create minimal config with defaults
		ac = &appconfig.AppConfig{}
		ac.ResolveDefaults(allServiceNames)
	}

	// Build Config.Services[] from fully-resolved appconfig
	// IMPORTANT: Iterate over allServiceNames, not srvMap, because services
	// may have concurrency config without commands
	var services []core_v1alpha.Services
	for _, serviceName := range allServiceNames {
		svc := core_v1alpha.Services{
			Name: serviceName,
		}

		// Map from appconfig to entity schema
		// After ResolveDefaults(), every service is guaranteed to have config
		if serviceConfig, ok := ac.Services[serviceName]; ok && serviceConfig != nil {
			// Copy image if specified
			if serviceConfig.Image != "" {
				svc.Image = serviceConfig.Image
			}

			// Copy port if specified
			if serviceConfig.Port > 0 {
				svc.Port = int64(serviceConfig.Port)
			}

			// Copy port name if specified
			if serviceConfig.PortName != "" {
				svc.PortName = serviceConfig.PortName
			}

			// Copy port type if specified
			if serviceConfig.PortType != "" {
				svc.PortType = serviceConfig.PortType
			}

			if serviceConfig.Concurrency != nil {
				svc.ServiceConcurrency = core_v1alpha.ServiceConcurrency{
					Mode:                serviceConfig.Concurrency.Mode,
					NumInstances:        int64(serviceConfig.Concurrency.NumInstances),
					RequestsPerInstance: int64(serviceConfig.Concurrency.RequestsPerInstance),
					ScaleDownDelay:      serviceConfig.Concurrency.ScaleDownDelay,
				}
			}

			// Convert disk configurations
			if len(serviceConfig.Disks) > 0 {
				svc.Disks = make([]core_v1alpha.Disks, 0, len(serviceConfig.Disks))
				for _, disk := range serviceConfig.Disks {
					svc.Disks = append(svc.Disks, core_v1alpha.Disks{
						Name:         disk.Name,
						MountPath:    disk.MountPath,
						ReadOnly:     disk.ReadOnly,
						SizeGb:       int64(disk.SizeGB),
						Filesystem:   disk.Filesystem,
						LeaseTimeout: disk.LeaseTimeout,
					})
				}
			}

			// Convert service-specific environment variables
			if len(serviceConfig.EnvVars) > 0 {
				svc.Env = make([]core_v1alpha.Env, 0, len(serviceConfig.EnvVars))
				for _, envVar := range serviceConfig.EnvVars {
					svc.Env = append(svc.Env, core_v1alpha.Env{
						Key:    envVar.Key,
						Value:  envVar.Value,
						Source: "config",
					})
				}
			}
		}

		services = append(services, svc)
	}

	return services
}

// ConfigInputs holds all the inputs needed to build an app version config.
type ConfigInputs struct {
	// BuildResult contains entrypoint, working dir, and image entrypoint/cmd from the build
	BuildResult *BuildResult

	// AppConfig is the parsed app.toml configuration (may be nil)
	AppConfig *appconfig.AppConfig

	// ProcfileServices maps service names to commands from the Procfile (may be nil)
	ProcfileServices map[string]string

	// ExistingConfig is the current config to preserve manual env vars from
	ExistingConfig core_v1alpha.Config
}

// buildVersionConfig builds the app version config from all inputs.
// This is a pure function that can be easily tested.
func buildVersionConfig(inputs ConfigInputs) core_v1alpha.Config {
	var cfg core_v1alpha.Config

	res := inputs.BuildResult
	ac := inputs.AppConfig
	procfileServices := inputs.ProcfileServices

	// Preserve existing variables for merging later
	cfg.Variable = inputs.ExistingConfig.Variable

	// Set entrypoint from stack build result
	if res != nil && res.Entrypoint != "" {
		cfg.Entrypoint = res.Entrypoint
	}

	// Set start directory from build result, defaulting to /app
	if res != nil && res.WorkingDir != "" {
		cfg.StartDirectory = res.WorkingDir
	} else {
		cfg.StartDirectory = "/app"
	}

	// If no web service defined in app config or Procfile, but we have an entrypoint or command,
	// create a synthetic Procfile entry for web service
	hasWebInAppConfig := ac != nil && ac.Services["web"] != nil && ac.Services["web"].Command != ""
	hasWebInProcfile := procfileServices != nil && procfileServices["web"] != ""
	if !hasWebInAppConfig && !hasWebInProcfile && res != nil {
		webCmd := res.Command
		if webCmd != "" {
			if procfileServices == nil {
				procfileServices = make(map[string]string)
			}
			procfileServices["web"] = webCmd
		}
	}

	// Build service configurations with concurrency settings from app.toml/Procfile
	cfg.Services = buildServicesConfig(ac, procfileServices)

	// Merge env vars: preserve manual vars from existing services
	existingServices := inputs.ExistingConfig.Services
	for i := range cfg.Services {
		serviceName := cfg.Services[i].Name

		// Find matching service in existing config
		for _, existingSvc := range existingServices {
			if existingSvc.Name == serviceName {
				// Merge env vars: app.toml vars override, but manual vars persist
				cfg.Services[i].Env = mergeServiceEnvVars(existingSvc.Env, cfg.Services[i].Env)
				break
			}
		}
	}

	// Build commands list for services that have explicit commands
	var serviceCmds []core_v1alpha.Commands
	for _, svc := range cfg.Services {
		// Check if this service has a command from app config or procfile
		var cmd string
		if ac != nil {
			if svcConfig, ok := ac.Services[svc.Name]; ok && svcConfig != nil && svcConfig.Command != "" {
				cmd = svcConfig.Command
			}
		}
		if cmd == "" {
			if procCmd, ok := procfileServices[svc.Name]; ok {
				cmd = procCmd
			}
		}

		if cmd != "" {
			serviceCmds = append(serviceCmds, core_v1alpha.Commands{
				Service: svc.Name,
				Command: cmd,
			})
		}
	}

	cfg.Commands = serviceCmds

	// Merge environment variables from app config
	// Preserves existing variables when app.toml has no [[env]] section
	cfg.Variable = mergeVariablesFromAppConfig(cfg.Variable, ac)

	return cfg
}

func buildVariablesFromAppConfig(appConfig *appconfig.AppConfig) []core_v1alpha.Variable {
	if appConfig == nil || len(appConfig.EnvVars) == 0 {
		return nil
	}

	variables := make([]core_v1alpha.Variable, 0, len(appConfig.EnvVars))
	for _, envVar := range appConfig.EnvVars {
		variables = append(variables, core_v1alpha.Variable{
			Key:    envVar.Key,
			Value:  envVar.Value,
			Source: "config",
		})
	}
	return variables
}

// mergeVariablesFromAppConfig merges environment variables from app.toml into existing variables.
// The merge strategy respects variable sources:
// - Manual vars (source="manual") always persist and shadow config vars with the same key
// - Variables from app.toml (source="config") override existing config vars but never manual vars
// - If a variable is removed from app.toml, it's only deleted if it was originally from config
// - If appConfig is nil or has no env vars, all existing variables are preserved.
func mergeVariablesFromAppConfig(existingVars []core_v1alpha.Variable, appConfig *appconfig.AppConfig) []core_v1alpha.Variable {
	appConfigVars := buildVariablesFromAppConfig(appConfig)

	// If no app.toml vars, preserve all existing vars
	if appConfigVars == nil {
		return existingVars
	}

	// Build a map of app.toml variables for quick lookup
	appConfigMap := make(map[string]core_v1alpha.Variable)
	for _, v := range appConfigVars {
		appConfigMap[v.Key] = v
	}

	// Build result by merging
	varMap := make(map[string]core_v1alpha.Variable)

	// First, add all existing manual variables - these always persist
	for _, v := range existingVars {
		// Backward compatibility: empty source is treated as "config"
		source := v.Source
		if source == "" {
			source = "config"
		}

		// Keep manual vars - they shadow config vars with the same key
		if source == "manual" {
			varMap[v.Key] = v
		}
		// config vars are only kept if still in app.toml (checked below)
	}

	// Now add app.toml variables, but never override manual vars
	for key, v := range appConfigMap {
		if _, hasManual := varMap[key]; !hasManual {
			varMap[key] = v
		}
	}

	// Convert map back to slice
	result := make([]core_v1alpha.Variable, 0, len(varMap))
	for _, v := range varMap {
		result = append(result, v)
	}

	return result
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
	if ac != nil {
		b.Log.Info("loaded app config", "name", ac.Name, "envVarCount", len(ac.EnvVars), "serviceCount", len(ac.Services))
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
		detectOpts := stackbuild.BuildOptions{
			Log:         b.Log,
			Name:        name,
			OnBuild:     buildStack.OnBuild,
			Version:     buildStack.Version,
			AlpineImage: buildStack.AlpineImage,
		}
		_, err := stackbuild.DetectStack(buildStack.CodeDir, detectOpts)
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
	rbk, err := lbk.Launch(ctx, ip.String(), WithLogEntity(name), WithAppName(name), WithLogAttrs(map[string]string{
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

	_, mrv, _, err := b.nextVersion(ctx, name)
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

	if res.ManifestDigest == "" {
		b.Log.Error("build did not return manifest digest")
		b.sendErrorStatus(ctx, status, "Build did not return manifest digest")
		return fmt.Errorf("build did not return manifest digest")
	}

	var artifact core_v1alpha.Artifact

	err = b.ec.OneAtIndex(ctx, entity.String(core_v1alpha.ArtifactManifestDigestId, res.ManifestDigest), &artifact)
	if err != nil {
		b.Log.Error("error locating artifact by digest", "digest", res.ManifestDigest, "error", err)
		return fmt.Errorf("error locating artifact by digest %s: %w", res.ManifestDigest, err)
	}

	b.Log.Debug("located stored artifact", "artifact", artifact.ID, "digest", res.ManifestDigest)

	mrv.Artifact = artifact.ID

	// Update ImageUrl to match the artifact we found (which may be reused due to deduplication)
	artifactName := strings.TrimPrefix(string(artifact.ID), "artifact/")
	mrv.ImageUrl = "cluster.local:5000/" + name + ":" + artifactName

	b.Log.Debug("build complete", "image", mrv.ImageUrl)

	procfileServices, err := b.readProcFile(tr)
	if err != nil {
		return fmt.Errorf("error reading procfile: %w", err)
	} else if procfileServices == nil {
		b.Log.Debug("no procfile found, using app config")
	} else {
		b.Log.Debug("using procfile", "services", maps.Keys(procfileServices))
	}

	// Build the version config from all inputs
	mrv.Config = buildVersionConfig(ConfigInputs{
		BuildResult:      res,
		AppConfig:        ac,
		ProcfileServices: procfileServices,
		ExistingConfig:   mrv.Config,
	})

	if ac != nil && len(ac.EnvVars) > 0 {
		b.Log.Info("merged env vars from app config", "count", len(ac.EnvVars))
	} else {
		b.Log.Debug("no new env vars from app config, preserving existing variables")
	}

	id, err := b.ec.Create(ctx, mrv.Version, mrv)
	if err != nil {
		return fmt.Errorf("error creating app version: %w", err)
	}

	b.Log.Info("updating app entity with new version", "app", name, "version", mrv.Version)
	err = b.appClient.SetActiveVersion(ctx, name, string(id))
	if err != nil {
		return fmt.Errorf("error updating app entity: %w", err)
	}

	b.Log.Info("app version updated", "app", name, "version", mrv.Version)

	// Log the deployment to the app's logs
	b.logDeployment(ctx, name, mrv.Version, artifactName)

	// Note: Old version pool cleanup is now handled by the DeploymentLauncher controller
	// via the referenced_by_versions field. The launcher removes version references and
	// scales down pools when they're no longer in use.

	state.Results().SetVersion(mrv.Version)

	// Get access info for the deployed app
	accessInfo := b.getAccessInfo(ctx, name)
	state.Results().SetAccessInfo(&accessInfo)

	return nil
}

// getAccessInfo queries routes to determine how the app can be accessed
func (b *Builder) getAccessInfo(ctx context.Context, appName string) *build_v1alpha.AccessInfo {
	info := &build_v1alpha.AccessInfo{}

	// Get the app entity to find its ID
	appEntity, err := b.appClient.GetByName(ctx, appName)
	if err != nil {
		b.Log.Debug("could not get app for access info", "app", appName, "error", err)
		return info
	}

	// Get all routes
	routes, err := b.ingressClient.List(ctx)
	if err != nil {
		b.Log.Debug("could not list routes for access info", "error", err)
		return info
	}

	// Filter routes for this app
	var hostnames []string
	var hasDefaultRoute bool

	for _, r := range routes {
		if r.Route.App != appEntity.ID {
			continue
		}
		if r.Route.Default {
			hasDefaultRoute = true
		}
		if r.Route.Host != "" {
			hostnames = append(hostnames, r.Route.Host)
		}
	}

	info.SetHostnames(&hostnames)
	info.SetDefaultRoute(hasDefaultRoute)

	// Include the cloud DNS hostname if available
	if b.DNSHostname != "" {
		info.SetClusterHostname(b.DNSHostname)
	}

	return info
}

func (b *Builder) logDeployment(ctx context.Context, appName, version, artifact string) {
	if b.LogWriter == nil {
		return
	}

	// Get app entity ID
	var appRec core_v1alpha.App
	err := b.ec.Get(ctx, appName, &appRec)
	if err != nil {
		b.Log.Warn("failed to get app for deployment logging", "app", appName, "error", err)
		return
	}

	// Format in Heroku logfmt style
	logMsg := fmt.Sprintf("version=%s artifact=%s status=deployed", version, artifact)

	err = b.LogWriter.WriteEntry(appRec.ID.String(), observability.LogEntry{
		Timestamp: time.Now(),
		Stream:    observability.UserOOB,
		Body:      logMsg,
		Attributes: map[string]string{
			"source":   "builder",
			"version":  version,
			"artifact": artifact,
		},
	})
	if err != nil {
		b.Log.Error("failed to write deployment log entry", "error", err, "app", appName)
	}
}

// buildImageCommand combines the OCI image entrypoint and cmd into a single shell command string.
// This is used when no Procfile or app config command is specified for a service.
func buildImageCommand(entrypoint, cmd []string) string {
	// Combine entrypoint and cmd
	var parts []string
	parts = append(parts, entrypoint...)
	parts = append(parts, cmd...)

	if len(parts) == 0 {
		return ""
	}

	// If there's only one part and it looks like a shell command, return it directly
	if len(parts) == 1 {
		return parts[0]
	}

	// For multiple parts, we need to properly quote them for shell execution
	// This handles cases like: ENTRYPOINT ["node"] CMD ["server.js"]
	// Which should become: node server.js
	var quotedParts []string
	for _, p := range parts {
		// If the part contains spaces or special characters, quote it
		if strings.ContainsAny(p, " \t\n\"'$`\\") {
			quotedParts = append(quotedParts, fmt.Sprintf("%q", p))
		} else {
			quotedParts = append(quotedParts, p)
		}
	}

	return strings.Join(quotedParts, " ")
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

// AnalyzeApp analyzes an app without building it, returning detected stack, services, and configuration.
func (b *Builder) AnalyzeApp(ctx context.Context, state *build_v1alpha.BuilderAnalyzeApp) error {
	args := state.Args()
	td := args.Tardata()

	path, err := os.MkdirTemp(b.TempDir, "analyze-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(path)

	b.Log.Debug("receiving tar data for analysis", "tempdir", path)

	r := stream.ToReader(ctx, td)

	tr, err := tarx.TarFS(r, path)
	if err != nil {
		return fmt.Errorf("error untaring data: %w", err)
	}

	result := &build_v1alpha.AnalysisResult{}

	// Collect detection events from multiple sources
	var events []build_v1alpha.DetectionEvent

	// Load app config
	ac, err := b.loadAppConfig(tr)
	if err != nil {
		b.Log.Warn("error loading app config, ignoring", "error", err)
	}
	if ac != nil {
		var event build_v1alpha.DetectionEvent
		event.SetKind("config")
		event.SetName("app.toml")
		event.SetMessage("Found app.toml configuration file")
		events = append(events, event)

		if ac.Name != "" {
			result.SetAppName(ac.Name)
		}

		// Extract env var keys (not values for security)
		var envKeys []string
		for _, ev := range ac.EnvVars {
			envKeys = append(envKeys, ev.Key)
		}
		if len(envKeys) > 0 {
			result.SetEnvVars(&envKeys)
		}

		// Check for explicit dockerfile in build config
		if ac.Build != nil && ac.Build.Dockerfile != "" {
			result.SetBuildDockerfile(ac.Build.Dockerfile)
		}
	}

	// Detect stack and build a BuildResult to use with buildVersionConfig
	var stackName string
	var buildResult BuildResult
	var detectedStack stackbuild.Stack

	// Check for Dockerfile.miren first
	if f, err := tr.Open("Dockerfile.miren"); err == nil {
		f.Close()
		stackName = "dockerfile"
		result.SetBuildDockerfile("Dockerfile.miren")
	} else if ac != nil && ac.Build != nil && ac.Build.Dockerfile != "" {
		stackName = "dockerfile"
	} else {
		// Try to detect stack
		var detectOpts stackbuild.BuildOptions
		detectOpts.Log = b.Log
		if ac != nil {
			detectOpts.Name = ac.Name
		}
		stack, err := stackbuild.DetectStack(path, detectOpts)
		if err != nil {
			b.Log.Debug("no stack detected", "error", err)
			stackName = "unknown"
		} else {
			detectedStack = stack
			stackName = stack.Name()
			buildResult.Entrypoint = stack.Entrypoint()
			buildResult.Command = stack.WebCommand()
			buildResult.WorkingDir = stack.Image().Config.WorkingDir
		}
	}

	result.SetStack(stackName)
	if buildResult.Entrypoint != "" {
		result.SetEntrypoint(buildResult.Entrypoint)
	}

	// Add detection events from the stack
	if detectedStack != nil {
		stackEvents := detectedStack.Events()
		for _, e := range stackEvents {
			var event build_v1alpha.DetectionEvent
			event.SetKind(e.Kind)
			event.SetName(e.Name)
			event.SetMessage(e.Message)
			events = append(events, event)
		}
	}

	// Read Procfile
	procfileServices, err := b.readProcFile(tr)
	if err != nil {
		return fmt.Errorf("error reading procfile: %w", err)
	}
	if len(procfileServices) > 0 {
		var event build_v1alpha.DetectionEvent
		event.SetKind("config")
		event.SetName("Procfile")
		event.SetMessage(fmt.Sprintf("Found Procfile with %d service(s)", len(procfileServices)))
		events = append(events, event)
	}

	// Use buildVersionConfig to compute services - same logic as BuildFromTar
	cfg := buildVersionConfig(ConfigInputs{
		BuildResult:      &buildResult,
		AppConfig:        ac,
		ProcfileServices: procfileServices,
	})

	result.SetWorkingDir(cfg.StartDirectory)

	// Build a map of commands for quick lookup
	commandMap := make(map[string]string)
	for _, cmd := range cfg.Commands {
		commandMap[cmd.Service] = cmd.Command
	}

	// Convert cfg.Services to ServiceInfo with source tracking
	// This includes ALL services, even those without explicit commands (they use image default)
	var services []build_v1alpha.ServiceInfo
	for _, svc := range cfg.Services {
		var svcInfo build_v1alpha.ServiceInfo
		svcInfo.SetName(svc.Name)

		if cmd, hasCommand := commandMap[svc.Name]; hasCommand {
			svcInfo.SetCommand(cmd)
			// Determine source for this service
			source := determineServiceSource(svc.Name, cmd, ac, procfileServices, &buildResult)
			svcInfo.SetSource(source)

			// Add event when we inject a synthetic web service from stack detection
			if svc.Name == "web" && source == "stack" {
				var event build_v1alpha.DetectionEvent
				event.SetKind("service")
				event.SetName("web")
				event.SetMessage("Injected web service from stack detection")
				events = append(events, event)
			}
		} else {
			// Service has no explicit command - uses Dockerfile CMD (image default)
			svcInfo.SetSource("image")
		}

		services = append(services, svcInfo)
	}

	if len(services) > 0 {
		result.SetServices(&services)
	}

	// Set all collected events
	if len(events) > 0 {
		result.SetEvents(&events)
	}

	state.Results().SetResult(&result)
	return nil
}

// determineServiceSource identifies where a service command came from
func determineServiceSource(serviceName, command string, ac *appconfig.AppConfig, procfileServices map[string]string, buildResult *BuildResult) string {
	// Check app config first
	if ac != nil {
		if svcConfig, ok := ac.Services[serviceName]; ok && svcConfig != nil && svcConfig.Command != "" {
			if svcConfig.Command == command {
				return "app_config"
			}
		}
	}

	// Check Procfile
	if procfileServices != nil {
		if procCmd, ok := procfileServices[serviceName]; ok && procCmd == command {
			return "procfile"
		}
	}

	// Must be from stack detection
	if buildResult != nil {
		webCmd := buildResult.Command
		if serviceName == "web" && command == webCmd {
			return "stack"
		}
	}

	return "unknown"
}
