package server

import (
	"context"
	"fmt"

	"miren.dev/runtime/api"
	"miren.dev/runtime/app"
	"miren.dev/runtime/lease"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/rpc/standard"
)

type RPCAppInfo struct {
	App   *app.AppAccess
	Lease *lease.LaunchContainer
	CPU   *metrics.CPUUsage
	Mem   *metrics.MemoryUsage
}

var _ api.AppInfo = &RPCAppInfo{}

func (a *RPCAppInfo) AppInfo(ctx context.Context, state *api.AppInfoAppInfo) error {
	args := state.Args()

	ac, err := a.App.LoadApp(ctx, args.Application())
	if err != nil {
		return fmt.Errorf("unknown application: %s", args.Application())
	}

	ai, err := a.Lease.AppInfo(ac.Xid)
	if err != nil {
		return err
	}

	var rai api.ApplicationStatus
	rai.SetName(ac.Name)

	ver, err := a.App.MostRecentVersion(ctx, ac)
	if err == nil {
		rai.SetActiveVersion(ver.Version)
		rai.SetLastDeploy(standard.ToTimestamp(ver.CreatedAt))
	}

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

	uats, err := a.CPU.CPUUsageLastHour(ac.Xid)
	if err != nil {
		return err
	}

	var usages []*api.CpuUsage

	for _, uat := range uats {
		var rcpu api.CpuUsage

		rcpu.SetStart(standard.ToTimestamp(uat.Timestamp))
		rcpu.SetCores(uat.Cores)

		usages = append(usages, &rcpu)
	}

	memusages, err := a.Mem.UsageLastHour(ac.Xid)
	if err != nil {
		return err
	}

	rai.SetCpuOverHour(usages)

	var musages []*api.MemoryUsage

	for _, mu := range memusages {
		var rmu api.MemoryUsage

		rmu.SetTimestamp(standard.ToTimestamp(mu.Timestamp))
		rmu.SetBytes(mu.Memory.Int64())

		musages = append(musages, &rmu)
	}

	rai.SetMemoryOverHour(musages)

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

	state.Results().SetStatus(&rai)

	return nil
}
