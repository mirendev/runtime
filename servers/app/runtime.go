package app

import (
	"context"
	"errors"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
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
		appVerEntity, err := a.EC.GetByIdWithEntity(ctx, appRec.ActiveVersion, &appVer)
		if err != nil {
			return err
		}
		rai.SetActiveVersion(appVer.Version)
		rai.SetLastDeploy(standard.ToTimestamp(appVerEntity.Entity().GetCreatedAt()))
	} else {
		appVer.App = appRec.ID
	}

	uats, err := a.CPU.CPUUsageLastHour(appRec.ID.String())
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

	memusages, err := a.Mem.UsageLastHour(appRec.ID.String())
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

	// Add HTTP request metrics
	if a.HTTP != nil {
		// Get current RPS
		rps, err := a.HTTP.RPSLastMinute(name)
		if err != nil {
			a.Log.Warn("failed to get RPS", "error", err)
			rps = 0
		}
		rai.SetRequestsPerSecond(rps)

		// Get request stats for the last hour
		stats, err := a.HTTP.StatsLastHour(name)
		if err != nil {
			a.Log.Warn("failed to get request stats", "error", err)
		} else {
			var requestStats []*app_v1alpha.RequestStat
			for _, s := range stats {
				var rs app_v1alpha.RequestStat
				rs.SetTimestamp(standard.ToTimestamp(s.Time))
				rs.SetCount(s.Count)
				rs.SetAvgDurationMs(s.AvgDurationMs)
				rs.SetErrorRate(s.ErrorRate)
				rs.SetP95DurationMs(s.P95DurationMs)
				rs.SetP99DurationMs(s.P99DurationMs)
				requestStats = append(requestStats, &rs)
			}
			rai.SetRequestStats(requestStats)
		}

		// Get top paths
		topPaths, err := a.HTTP.TopPaths(name, 5)
		if err != nil {
			a.Log.Warn("failed to get top paths", "error", err)
		} else {
			var pathStats []*app_v1alpha.PathStat
			for _, p := range topPaths {
				var ps app_v1alpha.PathStat
				ps.SetPath(p.Path)
				ps.SetCount(p.Count)
				ps.SetAvgDurationMs(p.AvgDurationMs)
				ps.SetErrorRate(p.ErrorRate)
				pathStats = append(pathStats, &ps)
			}
			rai.SetTopPaths(pathStats)
		}

		// Get error breakdown
		errorBreakdown, err := a.HTTP.ErrorsLastHour(name)
		if err != nil {
			a.Log.Warn("failed to get error breakdown", "error", err)
		} else {
			var errorStats []*app_v1alpha.ErrorBreakdown
			for _, e := range errorBreakdown {
				var es app_v1alpha.ErrorBreakdown
				es.SetStatusCode(int32(e.StatusCode))
				es.SetCount(e.Count)
				es.SetPercentage(e.Percentage)
				errorStats = append(errorStats, &es)
			}
			rai.SetErrorBreakdown(errorStats)
		}
	}

	// Get pool information from entity store
	if appVer.ID != "" {
		poolsResp, err := a.EC.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolReferencedByVersionsId, appVer.ID))
		if err != nil {
			a.Log.Warn("failed to get sandbox pools", "error", err)
		} else {
			var pools []*app_v1alpha.PoolStatus

			for poolsResp.Next() {
				var pool compute_v1alpha.SandboxPool
				poolsResp.Read(&pool)

				var rp app_v1alpha.PoolStatus
				rp.SetName(pool.Service)
				rp.SetIdle(int32(pool.ReadyInstances))
				rp.SetIdleUsage(0)

				var windows []*app_v1alpha.WindowStatus
				for i := int64(0); i < pool.CurrentInstances; i++ {
					var rw app_v1alpha.WindowStatus
					rw.SetVersion(appVer.Version)
					rw.SetLeases(1)
					rw.SetUsage(0)
					windows = append(windows, &rw)
				}

				rp.SetWindows(windows)
				pools = append(pools, &rp)
			}

			rai.SetPools(pools)
		}
	}

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
