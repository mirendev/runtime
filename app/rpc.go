package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"miren.dev/runtime/pkg/rpc/standard"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg app -input rpc.yml -output rpc.gen.go

type ClearVersioner interface {
	ClearOldVersions(ctx context.Context, current *AppVersion) error
}

type RPCCrud struct {
	Log    *slog.Logger
	CV     ClearVersioner
	Access *AppAccess
}

var _ Crud = &RPCCrud{}

func (r *RPCCrud) New(ctx context.Context, state *CrudNew) error {
	name := state.Args().Name()
	_, err := r.Access.LoadApp(ctx, name)
	if err == nil {
		// ok, return the current one.
		// TODO this is a bad id.
		state.Results().SetId(name)
		return nil
	}

	err = r.Access.CreateApp(ctx, &AppConfig{
		Name: name,
	})
	if err != nil {
		return err
	}

	// TODO this is a bad id.
	state.Results().SetId(name)

	return nil
}

func (r *RPCCrud) List(ctx context.Context, state *CrudList) error {
	apps, err := r.Access.ListApps(ctx)
	if err != nil {
		return err
	}

	var ai []*AppInfo

	for _, ac := range apps {
		var a AppInfo

		a.SetName(ac.Name)
		a.SetCreatedAt(standard.ToTimestamp(ac.CreatedAt))

		mrv, err := r.Access.MostRecentVersion(ctx, ac)
		if err == nil {
			var vi VersionInfo
			vi.SetVersion(mrv.Version)
			vi.SetCreatedAt(standard.ToTimestamp(mrv.CreatedAt))
			a.SetCurrentVersion(&vi)
		}

		ai = append(ai, &a)
	}

	state.Results().SetApps(ai)

	return nil
}

func (r *RPCCrud) SetConfiguration(ctx context.Context, state *CrudSetConfiguration) error {
	name := state.Args().App()
	ac, err := r.Access.LoadApp(ctx, name)
	if err != nil {
		return err
	}

	ver, err := r.Access.MostRecentVersion(ctx, ac)
	if err != nil {
		// Error out so that we don't create a version that has no image
		return err
	}

	cfg := state.Args().Configuration()

	if cfg.HasEnvVars() {
		for _, nv := range cfg.EnvVars() {
			if strings.HasPrefix(nv.Key(), "RUNTIME_") {
				return fmt.Errorf("cannot set RUNTIME_ environment variables")
			}
		}
	}

	ver.Configuration = cfg

	ver.Version = "" // Let create version assign one

	// By updating the existing version, we're implicitly reusing the same
	// image_id as the prev version, which is what we want.

	err = r.Access.CreateVersion(ctx, ver)
	if err != nil {
		return err
	}

	r.Log.Info("clearing old version", "app", name, "new-ver", ver.Version)
	err = r.CV.ClearOldVersions(ctx, ver)
	if err != nil {
		return err
	}

	state.Results().SetVersionId(ver.Version)

	return nil
}

func (r *RPCCrud) GetConfiguration(ctx context.Context, state *CrudGetConfiguration) error {
	name := state.Args().App()
	ac, err := r.Access.LoadApp(ctx, name)
	if err != nil {
		return err
	}

	ver, err := r.Access.MostRecentVersion(ctx, ac)
	if err != nil {
		state.Results().SetConfiguration(&Configuration{})
		return nil
	}

	state.Results().SetConfiguration(ver.Configuration)

	return nil
}

func (r *RPCCrud) SetHost(ctx context.Context, state *CrudSetHost) error {
	name := state.Args().App()
	ac, err := r.Access.LoadApp(ctx, name)
	if err != nil {
		return err
	}

	return r.Access.SetApplicationHost(ctx, ac, state.Args().Host())
}
