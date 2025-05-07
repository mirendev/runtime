package app

import (
	"context"
	"errors"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc/standard"
)

var _ app_v1alpha.AppStatus = &AppInfo{}

func (a *AppInfo) AppInfo(ctx context.Context, state *app_v1alpha.AppStatusAppInfo) error {
	name := state.Args().Application()

	var appRec core_v1alpha.App

	var rai app_v1alpha.ApplicationStatus
	rai.SetName(name)

	err := a.EC.Get(ctx, name, &appRec)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// No app, no status
			state.Results().SetStatus(&rai)
			return nil
		}

		return err
	}

	var appVer core_v1alpha.AppVersion

	if appRec.ActiveVersion != "" {
		err = a.EC.GetById(ctx, appRec.ActiveVersion, &appVer)
		if err != nil {
			return err
		}
		rai.SetActiveVersion(appVer.Version)
		//rai.SetLastDeploy(standard.ToTimestamp(ver.CreatedAt))
	} else {
		appVer.App = appRec.ID
	}

	/*
		ai, err := a.Lease.AppInfo(ac.Xid)
		if err != nil {
			return err
		}
	*/

	/*

		cores, err := a.CPU.CurrentCPUUsage(ac.Xid)
		if err != nil {
			return err
		}

		rai.SetLastMinCPU(cores)

		cores, err = a.CPU.CPUUsageOverLastHour(ac.Xid)
		if err != nil {
			return err
		}

		rai.SetLastHourCPU(cores)

		cores, err = a.CPU.CPUUsageOverDay(ac.Xid)
		if err != nil {
			return err
		}

		rai.SetLastDayCPU(cores)
	*/

	uats, err := a.CPU.CPUUsageLastHour(name)
	if err != nil {
		return err
	}

	var usages []*app_v1alpha.CpuUsage

	for _, uat := range uats {
		var rcpu app_v1alpha.CpuUsage

		rcpu.SetStart(standard.ToTimestamp(uat.Timestamp))
		rcpu.SetCores(uat.Cores)

		usages = append(usages, &rcpu)
	}

	memusages, err := a.Mem.UsageLastHour(name)
	if err != nil {
		return err
	}

	rai.SetCpuOverHour(usages)

	var musages []*app_v1alpha.MemoryUsage

	for _, mu := range memusages {
		var rmu app_v1alpha.MemoryUsage

		rmu.SetTimestamp(standard.ToTimestamp(mu.Timestamp))
		rmu.SetBytes(mu.Memory.Int64())

		musages = append(musages, &rmu)
	}

	rai.SetMemoryOverHour(musages)

	/*
		if ai == nil {
			state.Results().SetStatus(&rai)
			return nil
		}

		var pools []*api.PoolStatus

		for _, p := range ai.Pools {
			var rp api.PoolStatus

			rp.SetName(p.Name)
			rp.SetIdle(int32(p.Idle))
			rp.SetIdleUsage(int64(p.IdleUsage))

			var windows []*api.WindowStatus

			for _, w := range p.Windows {
				var rw api.WindowStatus

				rw.SetVersion(w.Version)
				rw.SetLeases(int32(w.Leases))
				rw.SetUsage(int64(w.Usage))

				windows = append(windows, &rw)
			}

			rp.SetWindows(windows)

			pools = append(pools, &rp)
		}

		rai.SetPools(pools)
	*/

	// Get instances from DB
	/*
		instances, err := a.DB.ListInstancesForApp(ac.Id)
		if err != nil {
			return err
		}

		// Convert to API format
		addons := make([]*api.AddonInstance, len(instances))
		for i, instance := range instances {
			apiInstance := &api.AddonInstance{}
			apiInstance.SetId(instance.Xid)
			apiInstance.SetName(instance.Apps[0].Name)
			apiInstance.SetAddon(instance.Addon)
			apiInstance.SetPlan(instance.Plan)
			addons[i] = apiInstance
		}

		rai.SetAddons(addons)
	*/

	state.Results().SetStatus(&rai)

	return nil
}
