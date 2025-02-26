package server

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/addons"
	"miren.dev/runtime/api"
	"miren.dev/runtime/app"
	"miren.dev/runtime/pkg/asm/autoreg"
)

type RPCAddons struct {
	Log *slog.Logger
	DB  *addons.DB
	App *app.AppAccess
	CV  app.ClearVersioner

	Reg *addons.Registry
}

var _ = autoreg.Register[RPCAddons]()

var _ api.Addons = &RPCAddons{}

// CreateInstance implements the Addons interface
func (a *RPCAddons) CreateInstance(ctx context.Context, state *api.AddonsCreateInstance) error {
	args := state.Args()
	results := state.Results()

	ac, err := a.App.LoadApp(ctx, args.App())
	if err != nil {
		return err
	}

	// Get addon
	addon := a.Reg.Get(args.Addon())
	if addon == nil {
		return fmt.Errorf("addon not found: %s", args.Addon())
	}

	// Get Plan
	var plan addons.Plan

	if args.Plan() == "" {
		plan = addon.Default()
	} else {
		for _, p := range addon.Plans() {
			if p.Name() == args.Plan() {
				plan = p
				break
			}
		}
	}

	if plan == nil {
		return fmt.Errorf("plan not found: %s", args.Plan())
	}

	name := args.Name()
	if name == "" {
		name = args.Addon()
	}

	a.Log.Info("Provisioning addon", "name", name, "addon", args.Addon(), "plan", plan.Name())

	// Provision instance
	cfg, err := addon.Provision(ctx, name, plan)
	if err != nil {
		return err
	}

	// Create instance in DB
	instance := &addons.Instance{
		Xid:         string(cfg.Id),
		Plan:        plan.Name(),
		Addon:       args.Addon(),
		ContainerId: cfg.Container,
		Config:      cfg,

		Apps: []addons.AppAttachment{
			{
				AppId: ac.Id,
				Name:  name,
			},
		},
	}

	err = a.DB.CreateInstance(instance)
	if err != nil {
		a.Log.Error("Failed to create instance", "error", err)
		derr := addon.Deprovision(ctx, cfg)
		if derr != nil {
			a.Log.Error("Failed to deprovision addon", "error", err)
		}
		return err
	}

	// Attach the addon to the app
	ver, err := a.App.MostRecentVersion(ctx, ac)
	if err != nil {
		// This will create a version without an image id, so it won't be
		// deployable.
		ver = &app.AppVersion{
			App:   ac,
			AppId: ac.Id,
		}
	}

	for k, v := range cfg.Env {
		if !ver.Configuration.HasEnvVar(k) {
			ver.Configuration.AddSensitiveEnvVar(k, v)
		}
	}

	ver.Version = ""

	err = a.App.CreateVersion(ctx, ver)
	if err != nil {
		return err
	}

	// Return ID
	results.SetId(instance.Xid)
	return nil
}

// DeleteInstance implements the Addons interface
func (a *RPCAddons) DeleteInstance(ctx context.Context, state *api.AddonsDeleteInstance) error {
	args := state.Args()

	// Load the app
	ac, err := a.App.LoadApp(ctx, args.App())
	if err != nil {
		return fmt.Errorf("failed to load app: %w", err)
	}

	// Get the instance by app and name
	instance, err := a.DB.GetInstanceByAppAndName(ac.Id, args.Name())
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	// Get the addon
	addon := a.Reg.Get(instance.Addon)
	if addon == nil {
		return fmt.Errorf("addon not found: %s", instance.Addon)
	}

	// Deprovision the instance
	err = addon.Deprovision(ctx, instance.Config)
	if err != nil {
		return fmt.Errorf("failed to deprovision instance: %w", err)
	}

	a.Log.Info("Deprovisioned addon instance", "id", args.Name())

	// Delete from database
	err = a.DB.DeleteInstance(instance.Id)
	if err != nil {
		return fmt.Errorf("failed to delete instance from database: %w", err)
	}

	a.Log.Info("Deleted addon instance", "id", args.Name())

	// Update Configuration to remove the env vars

	ver, err := a.App.MostRecentVersion(ctx, ac)
	if err != nil {
		return fmt.Errorf("failed to get most recent version: %w", err)
	}

	for k := range instance.Config.Env {
		ver.Configuration.RemoveEnvVar(k)
	}

	ver.Version = ""

	err = a.App.CreateVersion(ctx, ver)
	if err != nil {
		return fmt.Errorf("failed to create version: %w", err)
	}

	a.Log.Info("clearing old version", "app", args.App(), "new-ver", ver.Version)
	err = a.CV.ClearOldVersions(ctx, ver)
	if err != nil {
		return err
	}

	return nil
}

// ListInstances implements the Addons interface
func (a *RPCAddons) ListInstances(ctx context.Context, state *api.AddonsListInstances) error {
	args := state.Args()
	results := state.Results()

	ac, err := a.App.LoadApp(ctx, args.App())
	if err != nil {
		return err
	}

	// Get instances from DB
	instances, err := a.DB.ListInstancesForApp(ac.Id)
	if err != nil {
		return err
	}

	// Convert to API format
	apiInstances := make([]*api.AddonInstance, len(instances))
	for i, instance := range instances {
		apiInstance := &api.AddonInstance{}
		apiInstance.SetId(instance.Xid)
		apiInstance.SetName(instance.Apps[0].Name)
		apiInstance.SetAddon(instance.Addon)
		apiInstance.SetPlan(instance.Plan)
		apiInstances[i] = apiInstance
	}

	results.SetAddons(apiInstances)
	return nil
}
