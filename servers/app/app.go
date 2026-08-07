package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/pkg/workloadroles"
)

// TODO: Removed broken go:generate directive - no rpc.yml file exists in servers/app/
// If RPC generation is needed here, create rpc.yml first
// //go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg app -input rpc.yml -output rpc.gen.go

type ClearVersioner interface {
	ClearOldVersions(ctx context.Context, current *core_v1alpha.AppVersion) error
}

type AppInfo struct {
	Log  *slog.Logger
	CV   ClearVersioner
	EC   *entityserver.Client
	CPU  *metrics.CPUUsage
	Mem  *metrics.MemoryUsage
	HTTP *metrics.HTTPMetrics
}

func NewAppInfo(log *slog.Logger, ec *entityserver.Client, cpu *metrics.CPUUsage, mem *metrics.MemoryUsage, http *metrics.HTTPMetrics) *AppInfo {
	return &AppInfo{
		Log:  log,
		CV:   nil,
		EC:   ec,
		CPU:  cpu,
		Mem:  mem,
		HTTP: http,
	}
}

var _ app_v1alpha.Crud = &AppInfo{}

// versionShortId looks up the short ID for a version entity by its full ID.
func (r *AppInfo) versionShortId(ctx context.Context, versionId string) string {
	var v core_v1alpha.AppVersion
	ent, err := r.EC.GetByIdWithEntity(ctx, entity.Id(versionId), &v)
	if err != nil {
		return ""
	}
	return shortIDFromEntity(ent)
}

func shortIDFromEntity(ent *entityserver_v1alpha.Entity) string {
	if ent == nil {
		return ""
	}
	for _, attr := range ent.Attrs() {
		if entity.Id(attr.ID) == entity.DBShortId {
			return attr.Value.String()
		}
	}
	return ""
}

func (r *AppInfo) New(ctx context.Context, state *app_v1alpha.CrudNew) error {
	name := state.Args().Name()

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err == nil {
		// App already exists, return its ID
		state.Results().SetId(string(appRec.ID))
		return nil
	}
	if !errors.Is(err, cond.ErrNotFound{}) {
		return fmt.Errorf("failed to look up app %q: %w", name, err)
	}

	// Set default project to match the build server behavior
	appRec.Project = "project/default"

	id, err := r.EC.Create(ctx, name, &appRec)
	if err != nil {
		return err
	}

	state.Results().SetId(string(id))

	return nil
}

func (r *AppInfo) Destroy(ctx context.Context, state *app_v1alpha.CrudDestroy) error {
	name := state.Args().Name()

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// No app, no problem.
			return nil
		}

		return err
	}

	return DeleteAppTransitive(ctx, r.EC, r.Log, appRec.ID)
}

func (r *AppInfo) List(ctx context.Context, state *app_v1alpha.CrudList) error {
	list, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return err
	}

	// Collect apps and resolve their active versions
	type appEntry struct {
		name                string
		app                 core_v1alpha.App
		activeVersion       *core_v1alpha.AppVersion
		activeVersionEntity *entityserver_v1alpha.Entity
	}

	var apps []appEntry
	specMap := make(map[string]*core_v1alpha.ConfigSpec)

	for list.Next() {
		var app core_v1alpha.App
		list.Read(&app)
		md := list.Metadata()

		entry := appEntry{name: md.Name, app: app}

		if app.ActiveVersion != "" {
			var appVer core_v1alpha.AppVersion
			if verEnt, err := r.EC.GetByIdWithEntity(ctx, entity.Id(app.ActiveVersion), &appVer); err == nil {
				entry.activeVersion = &appVer
				entry.activeVersionEntity = verEnt
				if resolvedCfg, err := coreutil.ResolveConfig(ctx, r.EC.EAC(), &appVer); err == nil {
					specMap[appVer.ID.String()] = resolvedCfg
				}
			}
		}

		apps = append(apps, entry)
	}

	// Aggregate sandbox pool state per app
	poolStateMap := make(map[string]*poolHealth)

	poolList, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return err
	}

	now := time.Now()
	for poolList.Next() {
		var pool compute_v1alpha.SandboxPool
		poolList.Read(&pool)

		appName := ui.CleanEntityID(pool.App.String())
		if poolStateMap[appName] == nil {
			poolStateMap[appName] = &poolHealth{isAutoscale: true}
		}
		ps := poolStateMap[appName]
		ps.accumulate(&pool, now)

		if spec, ok := specMap[pool.SandboxSpec.Version.String()]; ok {
			for _, svc := range spec.Services {
				if svc.Name == pool.Service && svc.Concurrency.Mode == "fixed" {
					ps.isAutoscale = false
				}
			}
		}
	}

	// Collect routes per app
	routeMap := make(map[string][]string)

	routeList, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute))
	if err != nil {
		return err
	}

	for routeList.Next() {
		var route ingress_v1alpha.HttpRoute
		routeList.Read(&route)

		appName := ui.CleanEntityID(route.App.String())
		if route.Host != "" {
			routeMap[appName] = append(routeMap[appName], route.Host)
		} else if route.Default {
			routeMap[appName] = append(routeMap[appName], "")
		}
	}

	// Build response
	var results []*app_v1alpha.AppInfo

	for _, entry := range apps {
		var a app_v1alpha.AppInfo
		a.SetName(entry.name)

		if entry.activeVersion != nil {
			var vi app_v1alpha.VersionInfo
			vi.SetVersion(entry.activeVersion.Version)
			if sid := shortIDFromEntity(entry.activeVersionEntity); sid != "" {
				vi.SetShortId(sid)
			}
			a.SetCurrentVersion(&vi)
		}

		// Pool state → health, instances, scaling
		if ps, ok := poolStateMap[entry.name]; ok {
			a.SetReadyInstances(int32(ps.ready))
			a.SetDesiredInstances(int32(ps.desired))

			if ps.isAutoscale {
				a.SetScalingMode("auto")
			} else {
				a.SetScalingMode("fixed")
			}

			a.SetHealth(ps.classify())
			if ps.inCooldown {
				a.SetCrashCount(ps.crashCount)
				a.SetCooldownSeconds(int32(ps.cooldownLeft.Seconds()))
			}
		} else if entry.activeVersion != nil {
			// Active version but no pools yet. Mirror AppInfo so `m app list`
			// and app-status/deploy agree: an autoscale app reads as idle
			// (deliberately at zero), while a fixed service reads as starting
			// (not up yet) rather than a misleading idle.
			ps := poolHealth{isAutoscale: specAllowsScaleToZero(specMap[entry.activeVersion.ID.String()])}
			a.SetReadyInstances(0)
			a.SetDesiredInstances(0)
			a.SetHealth(ps.classify())
		} else {
			a.SetHealth(apphealth.Unknown)
		}

		// Routes
		if routes, ok := routeMap[entry.name]; ok {
			a.SetRoutes(routes)
		}

		results = append(results, &a)
	}

	state.Results().SetApps(results)

	return nil
}

func (r *AppInfo) SetConfiguration(ctx context.Context, state *app_v1alpha.CrudSetConfiguration) error {
	name := state.Args().App()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	// The read-merge-write below is retried under optimistic concurrency
	// control: the active_version swing is guarded by a CAS on the app revision,
	// so a concurrent writer (another SetConfiguration, an env mutation, or the
	// addon controller injecting addon vars) that swings active_version first is
	// detected and re-merged instead of silently clobbered.
	//
	// maxAttempts is a live-lock backstop, not an expected limit: there are only
	// ever a handful of concurrent config writers per app, so it is set high
	// enough that genuine contention never spuriously fails, while still
	// guaranteeing termination rather than looping forever.
	const maxAttempts = 100
	for range maxAttempts {
		appEnt, err := r.EC.EAC().Get(ctx, "app/"+name)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				// No app, no problem.
				return nil
			}

			return err
		}

		var appRec core_v1alpha.App
		appRec.Decode(appEnt.Entity().Entity())
		appRev := appEnt.Entity().Revision()

		var appVer core_v1alpha.AppVersion
		var spec core_v1alpha.ConfigSpec

		if appRec.ActiveVersion != "" {
			if err := r.EC.GetById(ctx, appRec.ActiveVersion, &appVer); err != nil {
				return err
			}
			resolvedCfg, err := coreutil.ResolveConfig(ctx, r.EC.EAC(), &appVer)
			if err != nil {
				return fmt.Errorf("failed to resolve config: %w", err)
			}
			spec = *resolvedCfg
		} else {
			appVer.App = appRec.ID
		}

		cfg := state.Args().Configuration()

		if cfg.HasEnvVars() {
			for _, nv := range cfg.EnvVars() {
				if strings.HasPrefix(nv.Key(), "MIREN_") {
					return fmt.Errorf("cannot set MIREN_ environment variables")
				}
			}
		}

		// Set commands directly on services
		for _, s := range cfg.Commands() {
			found := false
			for i := range spec.Services {
				if spec.Services[i].Name == s.Service() {
					spec.Services[i].Command = s.Command()
					found = true
					break
				}
			}
			if !found {
				spec.Services = append(spec.Services, core_v1alpha.ConfigSpecServices{
					Name:    s.Service(),
					Command: s.Command(),
				})
			}
		}

		// Replace the entire env var list with the new one from the client
		// The client is responsible for sending the complete desired state
		if cfg.HasEnvVars() {
			spec.Variables = nil
			for _, ev := range cfg.EnvVars() {
				source := ev.Source()
				nv := core_v1alpha.ConfigSpecVariables{
					Key:         ev.Key(),
					Value:       ev.Value(),
					Sensitive:   ev.Sensitive(),
					Source:      source,
					Required:    ev.Required(),
					Description: ev.Description(),
				}
				spec.Variables = append(spec.Variables, nv)
			}
		}

		// Handle per-service env vars
		if cfg.HasServices() {
			for _, svcCfg := range cfg.Services() {
				// Validate per-service env vars
				if svcCfg.HasServiceEnv() {
					for _, nv := range svcCfg.ServiceEnv() {
						if strings.HasPrefix(nv.Key(), "MIREN_") {
							return fmt.Errorf("cannot set MIREN_ environment variables")
						}
					}
				}

				// Find or create the service in spec.Services
				var found bool
				for i := range spec.Services {
					if spec.Services[i].Name == svcCfg.Service() {
						spec.Services[i].Env = nil
						if svcCfg.HasServiceEnv() {
							for _, ev := range svcCfg.ServiceEnv() {
								source := ev.Source()
								nv := core_v1alpha.ConfigSpecServicesEnv{
									Key:         ev.Key(),
									Value:       ev.Value(),
									Sensitive:   ev.Sensitive(),
									Source:      source,
									Required:    ev.Required(),
									Description: ev.Description(),
								}
								spec.Services[i].Env = append(spec.Services[i].Env, nv)
							}
						}
						found = true
						break
					}
				}

				if !found && svcCfg.HasServiceEnv() {
					svc := core_v1alpha.ConfigSpecServices{
						Name: svcCfg.Service(),
					}
					for _, ev := range svcCfg.ServiceEnv() {
						source := ev.Source()
						nv := core_v1alpha.ConfigSpecServicesEnv{
							Key:         ev.Key(),
							Value:       ev.Value(),
							Sensitive:   ev.Sensitive(),
							Source:      source,
							Required:    ev.Required(),
							Description: ev.Description(),
						}
						svc.Env = append(svc.Env, nv)
					}
					spec.Services = append(spec.Services, svc)
				}
			}
		}

		spec.Entrypoint = cfg.Entrypoint()

		appVer.Version = name + "-" + idgen.Gen("v")

		// Create ConfigVersion as the sole config store
		cvid, err := r.createConfigVersion(ctx, &spec, appVer.App, appVer.Version)
		if err != nil {
			return fmt.Errorf("error creating config version: %w", err)
		}
		appVer.ConfigVersion = cvid
		appVer.Config = core_v1alpha.Config{}

		avid, err := r.EC.Create(ctx, appVer.Version, &appVer)
		if err != nil {
			return err
		}

		// Swing active_version under OCC. A conflict means another writer got
		// there first, so re-read and re-merge on the next iteration.
		err = r.EC.Patch(ctx, appRec.ID, appRev,
			entity.Ref(core_v1alpha.AppActiveVersionId, avid),
		)
		if err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				// This attempt lost the race, so the version pair we just minted
				// was never activated. Best-effort delete it, AppVersion first:
				// only drop the ConfigVersion once its AppVersion is gone, so a
				// failed delete leaves a coherent pair for the version GC to reap
				// rather than an AppVersion dangling at a missing ConfigVersion.
				if delErr := r.EC.Delete(ctx, avid); delErr != nil {
					r.Log.Warn("failed to delete superseded app version after conflict",
						"app", appRec.ID, "version", avid, "error", delErr)
				} else if delErr := r.EC.Delete(ctx, cvid); delErr != nil {
					r.Log.Warn("failed to delete superseded config version after conflict",
						"app", appRec.ID, "config_version", cvid, "error", delErr)
				}
				continue
			}
			return fmt.Errorf("error updating app entity: %w", err)
		}

		state.Results().SetVersionId(appVer.Version)
		if sid := r.versionShortId(ctx, string(avid)); sid != "" {
			state.Results().SetVersionShortId(sid)
		}

		return nil
	}

	return fmt.Errorf("failed to set configuration on app %q after %d attempts due to concurrent writes", name, maxAttempts)
}

func (r *AppInfo) GetConfiguration(ctx context.Context, state *app_v1alpha.CrudGetConfiguration) error {
	name := state.Args().App()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err != nil {
		return err
	}

	var appVer core_v1alpha.AppVersion

	if appRec.ActiveVersion != "" {
		err = r.EC.GetById(ctx, appRec.ActiveVersion, &appVer)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("app has no active version")
	}

	spec, err := coreutil.ResolveConfig(ctx, r.EC.EAC(), &appVer)
	if err != nil {
		return fmt.Errorf("failed to resolve config: %w", err)
	}

	var cfg app_v1alpha.Configuration

	// Build commands from services that have commands
	var commands []*app_v1alpha.ServiceCommand
	for _, svc := range spec.Services {
		if svc.Command != "" {
			var sc app_v1alpha.ServiceCommand
			sc.SetService(svc.Name)
			sc.SetCommand(svc.Command)
			commands = append(commands, &sc)
		}
	}

	cfg.SetCommands(commands)

	var envVars []*app_v1alpha.NamedValue
	for _, ev := range spec.Variables {
		var env app_v1alpha.NamedValue
		env.SetKey(ev.Key)
		env.SetValue(ev.Value)
		env.SetSensitive(ev.Sensitive)
		env.SetSource(ev.Source)
		env.SetRequired(ev.Required)
		env.SetDescription(ev.Description)
		envVars = append(envVars, &env)
	}

	cfg.SetEnvVars(envVars)

	// Add per-service configurations
	var services []*app_v1alpha.ServiceConfig
	for _, svc := range spec.Services {
		var sc app_v1alpha.ServiceConfig
		sc.SetService(svc.Name)
		if svc.Concurrency.Mode != "" {
			sc.SetConcurrencyMode(svc.Concurrency.Mode)
		}
		if svc.Concurrency.NumInstances != 0 {
			sc.SetNumInstances(int32(svc.Concurrency.NumInstances))
		}

		// Add service env vars
		if len(svc.Env) > 0 {
			var svcEnvVars []*app_v1alpha.NamedValue
			for _, ev := range svc.Env {
				var env app_v1alpha.NamedValue
				env.SetKey(ev.Key)
				env.SetValue(ev.Value)
				env.SetSensitive(ev.Sensitive)
				env.SetSource(ev.Source)
				env.SetRequired(ev.Required)
				env.SetDescription(ev.Description)
				svcEnvVars = append(svcEnvVars, &env)
			}
			sc.SetServiceEnv(svcEnvVars)
		}

		services = append(services, &sc)
	}
	cfg.SetServices(services)

	cfg.SetEntrypoint(spec.Entrypoint)

	state.Results().SetConfiguration(&cfg)
	state.Results().SetVersionId(appVer.Version)
	if sid := r.versionShortId(ctx, string(appRec.ActiveVersion)); sid != "" {
		state.Results().SetVersionShortId(sid)
	}

	role := appRec.WorkloadRole
	if role == "" {
		role = workloadroles.Default
	}
	state.Results().SetWorkloadRole(role)

	return nil
}

func (r *AppInfo) SetHost(ctx context.Context, state *app_v1alpha.CrudSetHost) error {
	name := state.Args().App()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// No app, no problem.
			return nil
		}

		return err
	}

	var routeRec ingress_v1alpha.HttpRoute

	routeRec.Host = strings.ToLower(state.Args().Host())
	routeRec.App = appRec.ID

	_, err = r.EC.CreateOrUpdate(ctx, routeRec.Host, &routeRec)
	if err != nil {
		return err
	}

	return nil
}

// SetWorkloadRole sets the app's workload identity role. This is the operator
// path: it can grant cluster-scoped roles, which the app.toml path must not.
//
// It is reachable only by unscoped callers — cert (internal/operator) and
// JWT-with-RBAC. The guard below says that affirmatively: an app-scoped identity
// (OIDC or a workload) has a bound app, and is refused. That does not rely on
// the method staying absent from every token role map (the carve-out tripwire
// still enforces that too); it's belt-and-braces, and the shape the eventual
// cluster-admin gate will take.
func (r *AppInfo) SetWorkloadRole(ctx context.Context, state *app_v1alpha.CrudSetWorkloadRole) error {
	name := state.Args().App()
	role := state.Args().Role()

	if rpc.BoundApp(ctx) != "" {
		return rpc.AppAccessError(ctx, name)
	}

	if _, ok := workloadroles.Lookup(role); !ok {
		return fmt.Errorf("unknown workload role %q", role)
	}

	// Resolve the app to its entity ID (and confirm it exists).
	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, name, &appRec); err != nil {
		return err
	}

	// Patch only the workload_role attribute rather than Put-ing the whole App
	// record. A full-entity Update here would clobber a concurrent writer of the
	// same record — e.g. a deploy setting active_version between our Get and
	// Put. Patch is a single-attribute merge server-side, so there is no
	// read-modify-write to lose; revision 0 skips CAS accordingly.
	return r.EC.Patch(ctx, appRec.ID, 0, entity.String(core_v1alpha.AppWorkloadRoleId, role))
}

func (r *AppInfo) setEnvVars(ctx context.Context, name string, vars []appclient.EnvVarInput, service string) (string, error) {
	result, err := appclient.SetEnvVars(ctx, r.EC, name, nil, vars, service)
	if err != nil {
		return "", err
	}
	return result.VersionID, nil
}

func (r *AppInfo) SetEnvVar(ctx context.Context, state *app_v1alpha.CrudSetEnvVar) error {
	args := state.Args()

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	versionId, err := r.setEnvVars(ctx, args.App(), []appclient.EnvVarInput{
		{Key: args.Key(), Value: args.Value(), Sensitive: args.Sensitive()},
	}, args.Service())
	if err != nil {
		return err
	}

	state.Results().SetVersionId(versionId)
	if sid := r.versionShortId(ctx, versionId); sid != "" {
		state.Results().SetVersionShortId(sid)
	}
	return nil
}

func (r *AppInfo) SetEnvVars(ctx context.Context, state *app_v1alpha.CrudSetEnvVars) error {
	args := state.Args()

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	rpcVars := args.Vars()

	if len(rpcVars) == 0 {
		return fmt.Errorf("no environment variables provided")
	}

	vars := make([]appclient.EnvVarInput, len(rpcVars))
	for i, v := range rpcVars {
		vars[i] = appclient.EnvVarInput{Key: v.Key(), Value: v.Value(), Sensitive: v.Sensitive()}
	}

	versionId, err := r.setEnvVars(ctx, args.App(), vars, args.Service())
	if err != nil {
		return err
	}

	state.Results().SetVersionId(versionId)
	if sid := r.versionShortId(ctx, versionId); sid != "" {
		state.Results().SetVersionShortId(sid)
	}
	return nil
}

func (r *AppInfo) SetInitialEnvVars(ctx context.Context, state *app_v1alpha.CrudSetInitialEnvVars) error {
	args := state.Args()

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	rpcVars := args.Vars()

	if len(rpcVars) == 0 {
		return fmt.Errorf("no environment variables provided")
	}

	vars := make([]appclient.EnvVarInput, len(rpcVars))
	for i, v := range rpcVars {
		vars[i] = appclient.EnvVarInput{Key: v.Key(), Value: v.Value(), Sensitive: v.Sensitive()}
	}

	cvid, err := appclient.SetInitialEnvVars(ctx, r.EC, args.App(), vars, args.Service())
	if err != nil {
		return err
	}

	state.Results().SetConfigVersionId(string(cvid))
	return nil
}

func (r *AppInfo) DeleteEnvVar(ctx context.Context, state *app_v1alpha.CrudDeleteEnvVar) error {
	args := state.Args()

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	result, err := appclient.DeleteEnvVars(ctx, r.EC, args.App(), nil, []string{args.Key()}, args.Service())
	if err != nil {
		return err
	}

	state.Results().SetVersionId(result.VersionID)
	if sid := r.versionShortId(ctx, result.VersionID); sid != "" {
		state.Results().SetVersionShortId(sid)
	}
	if len(result.DeletedSources) > 0 {
		state.Results().SetDeletedSource(result.DeletedSources[0])
	}
	return nil
}

func (r *AppInfo) Restart(ctx context.Context, state *app_v1alpha.CrudRestart) error {
	args := state.Args()
	name := args.App()
	service := args.Service()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, name, &appRec); err != nil {
		return fmt.Errorf("app %q not found: %w", name, err)
	}

	// Resolve the config to restore DesiredInstances for fixed-mode pools.
	// During crash cooldown the pool manager resets DesiredInstances to 1;
	// we need to restore the configured value so fixed-mode pools come back
	// at the right scale.
	var configSpec *core_v1alpha.ConfigSpec
	if appRec.ActiveVersion != "" {
		var ver core_v1alpha.AppVersion
		if err := r.EC.GetById(ctx, appRec.ActiveVersion, &ver); err != nil {
			r.Log.Warn("failed to get active version, skipping desired instance restore",
				"version", appRec.ActiveVersion, "error", err)
		} else {
			spec, err := coreutil.ResolveConfig(ctx, r.EC.EAC(), &ver)
			if err != nil {
				r.Log.Warn("failed to resolve config, skipping desired instance restore", "error", err)
			} else {
				configSpec = spec
			}
		}
	}

	// Find all sandbox pools for this app
	poolList, err := r.EC.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, appRec.ID))
	if err != nil {
		return fmt.Errorf("listing pools: %w", err)
	}

	var restartedPools int32
	var stoppedSandboxes int32

	for poolList.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := poolList.Read(&pool); err != nil {
			continue
		}

		// Filter by service if specified
		if service != "" && pool.Service != service {
			continue
		}

		// Find and stop all RUNNING/PENDING sandboxes for this pool
		sbList, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
		if err != nil {
			r.Log.Warn("failed to list sandboxes", "pool", pool.ID, "error", err)
			continue
		}

		for sbList.Next() {
			var sb compute_v1alpha.Sandbox
			if err := sbList.Read(&sb); err != nil {
				continue
			}

			// Filter by pool label
			md := sbList.Metadata()
			if md == nil {
				continue
			}
			poolLabel, _ := md.Labels.Get("pool")
			if poolLabel != pool.ID.String() {
				continue
			}

			if sb.Status != compute_v1alpha.RUNNING && sb.Status != compute_v1alpha.PENDING {
				continue
			}

			if err := r.EC.Patch(ctx, sb.ID, 0,
				entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusStoppedId),
			); err != nil {
				// Surface stop failures loudly instead of swallowing them.
				// The original bug reported "0 sandboxes" success while
				// silently failing to stop anything; a restart that can't
				// stop a sandbox it found must not look like it worked.
				return fmt.Errorf("stopping sandbox %s: %w", sb.ID, err)
			}
			stoppedSandboxes++
		}

		// Build patch attrs: always reset crash cooldown fields
		patchAttrs := []entity.Attr{
			entity.Int64(compute_v1alpha.SandboxPoolConsecutiveCrashCountId, 0),
			entity.Time(compute_v1alpha.SandboxPoolLastCrashTimeId, time.Time{}),
			entity.Time(compute_v1alpha.SandboxPoolCooldownUntilId, time.Time{}),
		}

		// Restore DesiredInstances for fixed-mode pools that were capped to 1
		// during crash cooldown. Only do this for pools that reference the
		// active version — stale pools from old deployments were intentionally
		// scaled to 0 and should not be resurrected.
		isActivePool := slices.Contains(pool.ReferencedByVersions, appRec.ActiveVersion)
		if isActivePool && configSpec != nil {
			svcConc, err := coreutil.GetServiceConcurrency(configSpec, pool.Service)
			if err == nil && svcConc.Mode == "fixed" && svcConc.NumInstances > 0 {
				if pool.DesiredInstances != svcConc.NumInstances {
					patchAttrs = append(patchAttrs,
						entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, svcConc.NumInstances))
				}
			}
		}

		if err := r.EC.Patch(ctx, pool.ID, 0, patchAttrs...); err != nil {
			r.Log.Warn("failed to patch pool", "pool", pool.ID, "error", err)
		}

		restartedPools++
	}

	if restartedPools == 0 {
		if service != "" {
			return fmt.Errorf("no pools found for service %q of app %q", service, name)
		}
		return fmt.Errorf("no pools found for app %q", name)
	}

	r.Log.Info("app restarted",
		"app", name,
		"service", service,
		"pools", restartedPools,
		"sandboxes_stopped", stoppedSandboxes)

	state.Results().SetRestartedPools(restartedPools)
	state.Results().SetStoppedSandboxes(stoppedSandboxes)
	return nil
}

// createConfigVersion creates a ConfigVersion entity from a ConfigSpec.
func (r *AppInfo) createConfigVersion(ctx context.Context, spec *core_v1alpha.ConfigSpec, appID entity.Id, versionName string) (entity.Id, error) {
	configVer := &core_v1alpha.ConfigVersion{
		App:  appID,
		Spec: *spec,
	}
	cvName := versionName + "-cfg"
	return r.EC.Create(ctx, cvName, configVer)
}
