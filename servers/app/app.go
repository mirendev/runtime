package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg app -input rpc.yml -output rpc.gen.go

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

func (r *AppInfo) New(ctx context.Context, state *app_v1alpha.CrudNew) error {
	name := state.Args().Name()

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err == nil {
		state.Results().SetId(name)
		return nil
	}

	_, err = r.EC.Create(ctx, name, &appRec)
	if err != nil {
		return err
	}

	// TODO this is a bad id.
	state.Results().SetId(name)

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

	/*
		ver := &AppVersion{
			App:   ac,
			AppId: ac.Id,

			// This is a special version that will be used to clear all versions
			Version: "final-for-destroy",
		}

		err = r.CV.ClearOldVersions(ctx, ver)
		if err != nil {
			return err
		}
	*/

	return r.EC.Delete(ctx, appRec.ID)
}

func (r *AppInfo) List(ctx context.Context, state *app_v1alpha.CrudList) error {
	list, err := r.EC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return err
	}

	var ai []*app_v1alpha.AppInfo

	for list.Next() {
		var app core_v1alpha.App
		list.Read(&app)

		md := list.Metadata()

		var a app_v1alpha.AppInfo

		a.SetName(md.Name)
		//a.SetCreatedAt(standard.ToTimestamp(list.Entity().CreatedAt))

		if app.ActiveVersion != "" {
			var appVer core_v1alpha.AppVersion
			err = r.EC.GetById(ctx, app.ActiveVersion, &appVer)
			if err != nil {
				return err
			}

			var vi app_v1alpha.VersionInfo
			vi.SetVersion(appVer.Version)
			a.SetCurrentVersion(&vi)
		}

		ai = append(ai, &a)
	}

	state.Results().SetApps(ai)

	return nil
}

func (r *AppInfo) SetConfiguration(ctx context.Context, state *app_v1alpha.CrudSetConfiguration) error {
	name := state.Args().App()

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// No app, no problem.
			return nil
		}

		return err
	}

	var appVer core_v1alpha.AppVersion

	if appRec.ActiveVersion != "" {
		err = r.EC.GetById(ctx, appRec.ActiveVersion, &appVer)
		if err != nil {
			return err
		}
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

	for _, s := range cfg.Commands() {
		cmd := core_v1alpha.Commands{
			Service: s.Service(),
			Command: s.Command(),
		}

		if !slices.Contains(appVer.Config.Commands, cmd) {
			appVer.Config.Commands = append(appVer.Config.Commands, cmd)
		}
	}

	for _, ev := range cfg.EnvVars() {
		nv := core_v1alpha.Variable{
			Key:       ev.Key(),
			Value:     ev.Value(),
			Sensitive: ev.Sensitive(),
		}

		if !slices.Contains(appVer.Config.Variable, nv) {
			appVer.Config.Variable = append(appVer.Config.Variable, nv)
		}
	}

	appVer.Config.Entrypoint = cfg.Entrypoint()

	appVer.Config.Concurrency.Fixed = int64(cfg.Concurrency())

	appVer.Version = name + "-" + idgen.Gen("v")

	avid, err := r.EC.Create(ctx, appVer.Version, &appVer)
	if err != nil {
		return err
	}

	// By updating the existing version, we're implicitly reusing the same
	// image_id as the prev version, which is what we want.

	/*
		r.Log.Info("clearing old version", "app", name, "new-ver", ver.Version)
		err = r.CV.ClearOldVersions(ctx, ver)
		if err != nil {
			return err
		}
	*/

	appRec.ActiveVersion = avid
	err = r.EC.Update(ctx, &appRec)
	if err != nil {
		return fmt.Errorf("error updating app entity: %w", err)
	}

	state.Results().SetVersionId(appVer.Version)

	return nil
}

func (r *AppInfo) GetConfiguration(ctx context.Context, state *app_v1alpha.CrudGetConfiguration) error {
	name := state.Args().App()

	var appRec core_v1alpha.App

	err := r.EC.Get(ctx, name, &appRec)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// No app, no problem.
			return nil
		}

		return err
	}

	var appVer core_v1alpha.AppVersion

	if appRec.ActiveVersion != "" {
		err = r.EC.GetById(ctx, appRec.ActiveVersion, &appVer)
		if err != nil {
			return err
		}
	} else {
		return nil
	}

	var cfg app_v1alpha.Configuration

	var commands []*app_v1alpha.ServiceCommand
	for _, s := range appVer.Config.Commands {
		var sc app_v1alpha.ServiceCommand
		sc.SetService(s.Service)
		sc.SetCommand(s.Command)

		commands = append(commands, &sc)
	}

	cfg.SetCommands(commands)

	var envVars []*app_v1alpha.NamedValue
	for _, ev := range appVer.Config.Variable {
		var env app_v1alpha.NamedValue
		env.SetKey(ev.Key)
		env.SetValue(ev.Value)
		env.SetSensitive(ev.Sensitive)
		envVars = append(envVars, &env)
	}

	cfg.SetEnvVars(envVars)

	cfg.SetEntrypoint(appVer.Config.Entrypoint)
	cfg.SetConcurrency(int32(appVer.Config.Concurrency.Fixed))

	state.Results().SetConfiguration(&cfg)

	return nil
}

func (r *AppInfo) SetHost(ctx context.Context, state *app_v1alpha.CrudSetHost) error {
	name := state.Args().App()

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

	routeRec.Host = state.Args().Host()
	routeRec.App = appRec.ID

	_, err = r.EC.CreateOrUpdate(ctx, routeRec.Host, &routeRec)
	if err != nil {
		return err
	}

	return nil
}
