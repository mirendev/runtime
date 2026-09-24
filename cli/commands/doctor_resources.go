package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

type doctorResources struct {
	pools     []compute_v1alpha.SandboxPool
	sandboxes []sandboxHealthRecord
	disks     []storage_v1alpha.Disk
	volumes   []storage_v1alpha.DiskVolume
}

func gatherDoctorResources(ctx *Context) (*doctorResources, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cl, err := ctx.rpcClient(probeCtx, "entities")
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	eac := entityserver_v1alpha.NewEntityAccessClient(cl)
	r := &doctorResources{}
	for _, kindName := range []string{"sandbox_pool", "sandbox", "disk", "disk_volume"} {
		kind, err := eac.LookupKind(probeCtx, kindName)
		if err != nil {
			return nil, fmt.Errorf("lookup %s: %w", kindName, err)
		}
		for cursor := ""; ; {
			page, err := eac.ListPage(probeCtx, kind.Attr(), cursor, 200)
			if err != nil {
				return nil, fmt.Errorf("list %s: %w", kindName, err)
			}
			for _, entry := range page.Values() {
				switch kindName {
				case "sandbox_pool":
					var pool compute_v1alpha.SandboxPool
					pool.Decode(entry.Entity())
					r.pools = append(r.pools, pool)
				case "sandbox":
					var sb compute_v1alpha.Sandbox
					sb.Decode(entry.Entity())
					var meta core_v1alpha.Metadata
					meta.Decode(entry.Entity())
					pool, _ := meta.Labels.Get("pool")
					r.sandboxes = append(r.sandboxes, sandboxHealthRecord{Sandbox: sb, Pool: pool, Updated: time.UnixMilli(entry.UpdatedAt())})
				case "disk":
					var disk storage_v1alpha.Disk
					disk.Decode(entry.Entity())
					r.disks = append(r.disks, disk)
				case "disk_volume":
					var volume storage_v1alpha.DiskVolume
					volume.Decode(entry.Entity())
					r.volumes = append(r.volumes, volume)
				}
			}
			cursor = page.Cursor()
			if cursor == "" {
				break
			}
		}
	}
	if err := probeCtx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

func gatherDoctorIndex(ctx *Context) (int64, error) {
	// This is a full-store scan, not a connectivity probe. Keep it bounded,
	// but allow a larger cluster enough time to finish.
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cl, err := ctx.rpcClient(probeCtx, "entities")
	if err != nil {
		return 0, err
	}
	defer cl.Close()
	result, err := entityserver_v1alpha.NewEntityAccessClient(cl).CheckIndexHealth(probeCtx)
	if err != nil {
		return 0, err
	}
	return result.OrphanedEntries(), nil
}

func resourceUnavailable(env *doctorEnv) (checkResult, bool) {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "(no cluster configured)"}, true
	}
	if env.connErr != nil {
		return checkResult{Status: checkSkip, Summary: "(server unreachable)"}, true
	}
	if env.resourcesErr != nil || env.resources == nil {
		return checkResult{Status: checkSkip, Summary: fmt.Sprintf("(resource scan unavailable: %v)", env.resourcesErr)}, true
	}
	return checkResult{}, false
}

func checkEntityIndexes(env *doctorEnv) checkResult {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "(no cluster configured)"}
	}
	if env.connErr != nil {
		return checkResult{Status: checkSkip, Summary: "(server unreachable)"}
	}
	if env.indexErr != nil {
		return checkResult{Status: checkWarn, Summary: "index scan unavailable", Problem: &ui.Diagnostic{
			Summary: "could not check for orphaned index entries", Detail: env.indexErr.Error(),
			Actions: []ui.Action{{Command: "miren version", Note: "check that the server supports index diagnostics"}, {Command: "miren debug reindex --dry-run", Note: "run a complete scan without the doctor deadline"}},
		}}
	}
	n := env.orphans
	if n == 0 {
		return checkResult{Status: checkOK, Summary: "no orphaned index entries"}
	}
	return checkResult{Status: checkWarn, Summary: fmt.Sprintf("%d orphaned index entries", n), Problem: &ui.Diagnostic{
		Summary: fmt.Sprintf("%d index entries point to missing entities", n),
		Actions: []ui.Action{{Command: "miren debug reindex --dry-run", Note: "inspect index drift"}, {Command: "miren debug reindex", Note: "repair indexes after review"}},
	}}
}

func checkSandboxes(env *doctorEnv) checkResult {
	if skip, unavailable := resourceUnavailable(env); unavailable {
		return skip
	}
	// The same service name may be used by several apps. Keep their counts separate.
	pools := append([]compute_v1alpha.SandboxPool(nil), env.resources.pools...)
	for i := range pools {
		if pools[i].App != "" {
			pools[i].Service = pools[i].App.String() + "/" + pools[i].Service
		}
	}
	services := summarizeServiceHealth(pools, env.resources.sandboxes, time.Now())
	var failing []string
	for _, svc := range services {
		if svc.CrashStreak >= 3 {
			failing = append(failing, fmt.Sprintf("%s (%d consecutive crashes, last in 10m)", svc.Service, svc.CrashStreak))
		}
	}
	if len(failing) == 0 {
		return checkResult{Status: checkOK, Summary: "no elevated recent sandbox crash streaks"}
	}
	return checkResult{Status: checkFail, Summary: strings.Join(failing, ", "), Problem: &ui.Diagnostic{
		Summary: "services have repeated sandbox failures",
		Detail:  strings.Join(failing, ", "),
		Actions: []ui.Action{{Command: "miren sandbox list --all", Note: "inspect dead sandboxes"}, {Command: "miren app status -a <app>", Note: "view exit codes and failure logs"}},
	}}
}

func checkPools(env *doctorEnv) checkResult {
	if skip, unavailable := resourceUnavailable(env); unavailable {
		return skip
	}
	var failing []string
	now := time.Now()
	for _, pool := range env.resources.pools {
		if pool.DesiredInstances > 0 && pool.ReadyInstances == 0 && pool.CooldownUntil.After(now) && pool.ConsecutiveCrashCount >= 2 {
			failing = append(failing, fmt.Sprintf("%s/%s (%d crashes, retry in %s)", pool.App, pool.Service, pool.ConsecutiveCrashCount, formatDuration(pool.CooldownUntil.Sub(now))))
		}
	}
	if len(failing) == 0 {
		return checkResult{Status: checkOK, Summary: "no pools in crash cooldown"}
	}
	sort.Strings(failing)
	return checkResult{Status: checkFail, Summary: strings.Join(failing, ", "), Problem: &ui.Diagnostic{
		Summary: "service pools are crash-looping", Detail: strings.Join(failing, ", "),
		Actions: []ui.Action{{Command: "miren sandbox-pool list", Note: "inspect failing pools"}, {Command: "miren sandbox list --all", Note: "find failed sandboxes"}},
	}}
}

func checkDisks(env *doctorEnv) checkResult {
	if skip, unavailable := resourceUnavailable(env); unavailable {
		return skip
	}
	var failing []string
	for _, disk := range env.resources.disks {
		if disk.Status == storage_v1alpha.ERROR {
			failing = append(failing, fmt.Sprintf("disk %s", disk.ID))
		}
	}
	for _, volume := range env.resources.volumes {
		if volume.ActualState == storage_v1alpha.DV_ERROR {
			failing = append(failing, fmt.Sprintf("volume %s: %s", volume.ID, volume.ErrorMessage))
		}
	}
	if len(failing) == 0 {
		return checkResult{Status: checkOK, Summary: "no disk or volume errors"}
	}
	sort.Strings(failing)
	return checkResult{Status: checkFail, Summary: strings.Join(failing, ", "), Problem: &ui.Diagnostic{
		Summary: "disk or volume errors detected", Detail: strings.Join(failing, ", "),
		Actions: []ui.Action{{Command: "miren debug disk list", Note: "inspect disk states"}},
	}}
}
